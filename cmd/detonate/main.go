// Command detonate executes the semguard detonation corpus against an
// OpenAI-compatible reader endpoint across stage configurations, and renders
// the docs/results.md matrix from saved results files.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semguard/internal/corpus"
	"github.com/c360studio/semguard/internal/reader"
	"github.com/c360studio/semguard/internal/report"
	"github.com/c360studio/semguard/internal/untrusted"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "matrix" {
		return runMatrix(args[1:], stdout, stderr)
	}

	fs := flag.NewFlagSet("detonate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	casesFlag := fs.String("cases", "corpus", "corpus directory or path to cases.json")
	frameFlag := fs.String("frame", "off", "frame stage switch: off|on|datamark|framing")
	datamarkFlag := fs.String("datamark", "", "optional override for datamark switch: on|off")
	verifyFlag := fs.String("verify", "free", "verify mode: free|schema")
	modelFlag := fs.String("model", os.Getenv("SEMGUARD_LLM_MODEL"), "reader model name (or SEMGUARD_LLM_MODEL)")
	baseURLFlag := fs.String("url", os.Getenv("SEMGUARD_LLM_URL"), "OpenAI-compatible base URL (or SEMGUARD_LLM_URL)")
	apiKeyFlag := fs.String("key", os.Getenv("SEMGUARD_LLM_KEY"), "API key (or SEMGUARD_LLM_KEY)")
	outFlag := fs.String("out", "", "output results JSON path")
	warmupFlag := fs.Int("warmup", 0, "number of warmup requests before timing corpus runs")
	timeoutFlag := fs.Duration("timeout", 60*time.Second, "per-request timeout")
	concurrencyFlag := fs.Int("concurrency", 2, "worker concurrency (default 2)")
	hardwareFlag := fs.String("hardware", defaultHardware(), "hardware/architecture recorded in results file")
	rankDeltaFlag := fs.Int("rank-delta", report.DefaultRankDelta, "score delta threshold for H3 (rank)")
	matrixOutFlag := fs.String("matrix", "", "render Markdown matrix to path from remaining result JSON arguments")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *matrixOutFlag != "" {
		return renderMatrixFiles(*matrixOutFlag, fs.Args(), stdout, stderr)
	}

	opts, err := parseFrameSwitches(*frameFlag, *datamarkFlag)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if *verifyFlag != string(reader.ModeFree) && *verifyFlag != string(reader.ModeSchema) {
		_, _ = fmt.Fprintf(stderr, "detonate: --verify must be free or schema, got %q\n", *verifyFlag)
		return 2
	}
	if *concurrencyFlag < 1 {
		_, _ = fmt.Fprintf(stderr, "detonate: --concurrency must be >= 1, got %d\n", *concurrencyFlag)
		return 2
	}
	if *warmupFlag < 0 {
		_, _ = fmt.Fprintf(stderr, "detonate: --warmup must be >= 0, got %d\n", *warmupFlag)
		return 2
	}
	if *timeoutFlag <= 0 {
		_, _ = fmt.Fprintf(stderr, "detonate: --timeout must be > 0, got %v\n", *timeoutFlag)
		return 2
	}
	if strings.TrimSpace(*outFlag) == "" {
		_, _ = fmt.Fprintln(stderr, "detonate: --out is required")
		return 2
	}
	if strings.TrimSpace(*modelFlag) == "" {
		_, _ = fmt.Fprintln(stderr, "detonate: --model (or SEMGUARD_LLM_MODEL) is required")
		return 2
	}
	if strings.TrimSpace(*baseURLFlag) == "" {
		_, _ = fmt.Fprintln(stderr, "detonate: --url (or SEMGUARD_LLM_URL) is required")
		return 2
	}
	endpointHost, err := sanitizeEndpointHost(*baseURLFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: invalid endpoint URL: %v\n", err)
		return 2
	}

	corpusDir, repoRoot := resolveCorpusDir(*casesFlag)
	cases, tasks, corpusSHA, err := corpus.LoadWithHash(corpusDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: load corpus: %v\n", err)
		return 1
	}

	taskByID := make(map[string]corpus.Task, len(tasks))
	var schemaByTask map[string][]byte
	var schemaSHAByTask map[string]string
	schemaByTask = make(map[string][]byte, len(tasks))
	schemaSHAByTask = make(map[string]string, len(tasks))

	for _, t := range tasks {
		taskByID[t.ID] = t
		schemaPath := t.Schema
		if !filepath.IsAbs(schemaPath) && repoRoot != "" {
			cand := filepath.Join(repoRoot, schemaPath)
			if _, statErr := os.Stat(cand); statErr == nil {
				schemaPath = cand
			}
		}
		sb, ssha, loadErr := reader.LoadSchema(schemaPath)
		if loadErr != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: load schema for task %s: %v\n", t.ID, loadErr)
			return 1
		}
		schemaByTask[t.ID] = sb
		schemaSHAByTask[t.ID] = ssha
	}

	client := &reader.Client{
		BaseURL:    *baseURLFlag,
		APIKey:     *apiKeyFlag,
		HTTPClient: &http.Client{},
	}
	mode := reader.Mode(*verifyFlag)

	// Execute warmup requests sequentially before timing corpus cases.
	if *warmupFlag > 0 && len(cases) > 0 {
		for i := 0; i < *warmupFlag; i++ {
			wc := cases[i%len(cases)]
			if _, _, err := executeOne(ctx, client, wc, taskByID[wc.Task], schemaByTask[wc.Task], opts, mode, *modelFlag, *timeoutFlag); err != nil {
				_, _ = fmt.Fprintf(stderr, "detonate: warmup %d (%s): %v\n", i+1, wc.ID, err)
				return 1
			}
		}
	}

	rawRuns := make([]report.RunResult, len(cases))
	var framingSHA string
	var firstErr error
	var errOnce sync.Once

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, len(cases))
	for i := range cases {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	workers := *concurrencyFlag
	if workers > len(cases) && len(cases) > 0 {
		workers = len(cases)
	}
	var fshaOnce sync.Once
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if runCtx.Err() != nil {
					return
				}
				c := cases[idx]
				t := taskByID[c.Task]
				rr, fsha, err := executeOne(runCtx, client, c, t, schemaByTask[c.Task], opts, mode, *modelFlag, *timeoutFlag)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("case %s: %w", c.ID, err)
						cancel()
					})
					return
				}
				fshaOnce.Do(func() { framingSHA = fsha })
				rawRuns[idx] = rr
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: %v\n", firstErr)
		return 1
	}

	evaluated, err := report.Evaluate(cases, rawRuns, *verifyFlag, *rankDeltaFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: evaluate: %v\n", err)
		return 1
	}

	var primaryTaskSHA, primarySchemaSHA string
	if len(tasks) > 0 {
		tsum := sha256.Sum256([]byte(tasks[0].Instruction))
		primaryTaskSHA = hex.EncodeToString(tsum[:])
		if mode == reader.ModeSchema {
			primarySchemaSHA = schemaSHAByTask[tasks[0].ID]
		}
	}

	resFile := report.File{
		Version:               1,
		Model:                 *modelFlag,
		EndpointHost:          endpointHost,
		Hardware:              *hardwareFlag,
		CorpusSHA256:          corpusSHA,
		TaskInstructionSHA256: primaryTaskSHA,
		FramingSHA256:         framingSHA,
		SchemaSHA256:          primarySchemaSHA,
		Stages: report.StageConfig{
			Datamark: opts.Datamark,
			Framing:  opts.Framing,
			Verify:   *verifyFlag,
		},
		RankDelta: *rankDeltaFlag,
		Runs:      evaluated,
	}

	if err := os.MkdirAll(filepath.Dir(*outFlag), 0o750); err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: mkdir %s: %v\n", filepath.Dir(*outFlag), err)
		return 2
	}
	if err := report.WriteFile(*outFlag, resFile); err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: write %s: %v\n", *outFlag, err)
		return 2
	}

	var hijackedCount int
	for _, r := range evaluated {
		if r.ControlOf == nil && r.Outcomes.Hijacked {
			hijackedCount++
		}
	}
	_, _ = fmt.Fprintf(stdout, "detonate: wrote %s (%d runs, %d hijacked)\n", *outFlag, len(evaluated), hijackedCount)
	return 0
}

func runMatrix(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("detonate matrix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outFlag := fs.String("out", "docs/results.md", "output Markdown matrix path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return renderMatrixFiles(*outFlag, fs.Args(), stdout, stderr)
}

func renderMatrixFiles(outPath string, inPaths []string, stdout, stderr io.Writer) int {
	if len(inPaths) == 0 {
		_, _ = fmt.Fprintln(stderr, "detonate matrix: at least one results JSON file is required")
		return 2
	}
	files := make([]report.File, 0, len(inPaths))
	for _, p := range inPaths {
		f, err := report.LoadFile(p)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "detonate matrix: %v\n", err)
			return 1
		}
		files = append(files, f)
	}
	md, err := report.RenderMarkdown(files)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate matrix: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate matrix: mkdir: %v\n", err)
		return 2
	}
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate matrix: write %s: %v\n", outPath, err)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "detonate matrix: wrote %s (%d configurations)\n", outPath, len(files))
	return 0
}

func executeOne(
	ctx context.Context,
	client *reader.Client,
	c corpus.Case,
	t corpus.Task,
	schemaJSON []byte,
	opts untrusted.Options,
	mode reader.Mode,
	model string,
	timeout time.Duration,
) (report.RunResult, string, error) {
	udocs := make([]untrusted.Document, len(c.Documents))
	for i, d := range c.Documents {
		udocs[i] = untrusted.Document{ID: d.ID, Text: d.Text}
	}
	prompt, err := untrusted.Frame(untrusted.Task{
		Instruction: t.Instruction,
		Question:    c.Question,
	}, udocs, opts)
	if err != nil {
		return report.RunResult{}, "", fmt.Errorf("frame: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.Complete(reqCtx, reader.Request{
		Model:      model,
		System:     prompt.System,
		User:       prompt.User,
		Mode:       mode,
		TaskID:     c.Task,
		SchemaJSON: schemaJSON,
	})
	durMs := time.Since(start).Milliseconds()
	if err != nil {
		return report.RunResult{}, "", err
	}

	psum := sha256.Sum256([]byte(prompt.System + "\x00" + prompt.User))
	return report.RunResult{
		CaseID:       c.ID,
		Class:        c.Class,
		Task:         c.Task,
		ControlOf:    c.ControlOf,
		SystemPrompt: prompt.System,
		UserPrompt:   prompt.User,
		PromptSHA256: hex.EncodeToString(psum[:]),
		RawResponse:  resp.Content,
		FinishReason: resp.FinishReason,
		Refused:      resp.Refused,
		Refusal:      resp.Refusal,
		DurationMs:   durMs,
	}, prompt.FramingSHA256, nil
}

func parseFrameSwitches(frameFlag, datamarkFlag string) (untrusted.Options, error) {
	var opts untrusted.Options
	switch frameFlag {
	case "off", "none":
		opts.Datamark = false
		opts.Framing = false
	case "on", "both":
		opts.Datamark = true
		opts.Framing = true
	case "datamark":
		opts.Datamark = true
		opts.Framing = false
	case "framing":
		opts.Datamark = false
		opts.Framing = true
	default:
		return untrusted.Options{}, fmt.Errorf("detonate: --frame must be off|on|datamark|framing, got %q", frameFlag)
	}
	if datamarkFlag != "" {
		switch datamarkFlag {
		case "on":
			opts.Datamark = true
		case "off":
			opts.Datamark = false
		default:
			return untrusted.Options{}, fmt.Errorf("detonate: --datamark must be on|off, got %q", datamarkFlag)
		}
	}
	return opts, nil
}

func sanitizeEndpointHost(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("missing scheme or host")
	}
	return u.Scheme + "://" + u.Host, nil
}

func resolveCorpusDir(casesFlag string) (corpusDir, repoRoot string) {
	if strings.HasSuffix(casesFlag, ".json") {
		dir := filepath.Dir(casesFlag)
		return dir, filepath.Dir(dir)
	}
	return casesFlag, filepath.Dir(casesFlag)
}

func defaultHardware() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
