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
	"sync/atomic"
	"time"

	"github.com/c360studio/semguard/internal/audit"
	"github.com/c360studio/semguard/internal/corpus"
	"github.com/c360studio/semguard/internal/reader"
	"github.com/c360studio/semguard/internal/report"
	"github.com/c360studio/semguard/untrusted"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

const maxTrials = 10

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
	auditFlag := fs.String("audit", "off", "audit stage switch: off|semselect|chat")
	semselectURLFlag := fs.String("semselect-url", os.Getenv("SEMGUARD_SEMSELECT_URL"), "semselect base URL (or SEMGUARD_SEMSELECT_URL)")
	semselectKeyFlag := fs.String("semselect-key", os.Getenv("SEMGUARD_SEMSELECT_KEY"), "semselect API key (or SEMGUARD_SEMSELECT_KEY)")
	semselectRevFlag := fs.String("semselect-revision", os.Getenv("SEMGUARD_SEMSELECT_REVISION"), "semselect git revision recorded in results")
	semselectModelFlag := fs.String("semselect-model-lock", os.Getenv("SEMGUARD_SEMSELECT_MODEL_LOCK"), "semselect models.lock.json entry recorded in results")
	auditChatModelFlag := fs.String("audit-chat-model", os.Getenv("SEMGUARD_AUDIT_CHAT_MODEL"), "small chat model ID for --audit chat (or SEMGUARD_AUDIT_CHAT_MODEL)")
	auditThresholdFlag := fs.Float64("audit-threshold", audit.DefaultThreshold, "probability threshold for --audit semselect (default 0.5)")
	modelFlag := fs.String("model", os.Getenv("SEMGUARD_LLM_MODEL"), "reader model name (or SEMGUARD_LLM_MODEL)")
	baseURLFlag := fs.String("url", os.Getenv("SEMGUARD_LLM_URL"), "OpenAI-compatible base URL (or SEMGUARD_LLM_URL)")
	apiKeyFlag := fs.String("key", os.Getenv("SEMGUARD_LLM_KEY"), "API key (or SEMGUARD_LLM_KEY)")
	outFlag := fs.String("out", "", "output results JSON path")
	fromResultsFlag := fs.String("from-results", "", "optional existing results JSON path to re-audit the exact same reader records without re-running the reader")
	warmupFlag := fs.Int("warmup", 0, "number of warmup requests before timing corpus runs")
	timeoutFlag := fs.Duration("timeout", 60*time.Second, "per-request timeout")
	concurrencyFlag := fs.Int("concurrency", 2, "worker concurrency (default 2)")
	trialsFlag := fs.Int("trials", 1, "times to run the corpus (1..10); each trial pairs rank cases with that trial's control twins")
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
	if *auditFlag != "off" && *auditFlag != "semselect" && *auditFlag != "chat" {
		_, _ = fmt.Fprintf(stderr, "detonate: --audit must be off|semselect|chat, got %q\n", *auditFlag)
		return 2
	}
	if *concurrencyFlag < 1 {
		_, _ = fmt.Fprintf(stderr, "detonate: --concurrency must be >= 1, got %d\n", *concurrencyFlag)
		return 2
	}
	// The cap bounds the eager run allocation and the int product cases*trials; #30 asks for 3.
	if *trialsFlag < 1 || *trialsFlag > maxTrials {
		_, _ = fmt.Fprintf(stderr, "detonate: --trials must be 1..%d, got %d\n", maxTrials, *trialsFlag)
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
	if strings.TrimSpace(*fromResultsFlag) == "" && strings.TrimSpace(*modelFlag) == "" {
		_, _ = fmt.Fprintln(stderr, "detonate: --model (or SEMGUARD_LLM_MODEL) is required")
		return 2
	}
	if (strings.TrimSpace(*fromResultsFlag) == "" || *auditFlag == "chat") && strings.TrimSpace(*baseURLFlag) == "" {
		_, _ = fmt.Fprintln(stderr, "detonate: --url (or SEMGUARD_LLM_URL) is required")
		return 2
	}
	var endpointHost string
	if strings.TrimSpace(*baseURLFlag) != "" {
		endpointHost, err = sanitizeEndpointHost(*baseURLFlag)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: invalid endpoint URL: %v\n", err)
			return 2
		}
	}

	client := &reader.Client{
		BaseURL:    *baseURLFlag,
		APIKey:     *apiKeyFlag,
		HTTPClient: &http.Client{},
	}
	auditor, auditMeta, err := buildAuditor(*auditFlag, *semselectURLFlag, *semselectKeyFlag, *semselectRevFlag, *semselectModelFlag, *auditChatModelFlag, *auditThresholdFlag, endpointHost, client)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "detonate: %v\n", err)
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

	mode := reader.Mode(*verifyFlag)

	var primaryTaskSHA, primarySchemaSHA string
	if len(tasks) > 0 {
		tsum := sha256.Sum256([]byte(tasks[0].Instruction))
		primaryTaskSHA = hex.EncodeToString(tsum[:])
		if mode == reader.ModeSchema {
			primarySchemaSHA = schemaSHAByTask[tasks[0].ID]
		}
	}

	var evaluated []report.RunResult
	var framingSHA string
	var modelName = *modelFlag
	var hwName = *hardwareFlag
	var readerEndpointHost = endpointHost
	var outRankDelta = *rankDeltaFlag
	var outTrials = *trialsFlag
	if strings.TrimSpace(*fromResultsFlag) != "" {
		baseFile, loadErr := report.LoadFile(*fromResultsFlag)
		if loadErr != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: load --from-results %s: %v\n", *fromResultsFlag, loadErr)
			return 1
		}
		if baseFile.CorpusSHA256 != corpusSHA {
			_, _ = fmt.Fprintf(stderr, "detonate: --from-results corpus_sha256 %q != current corpus %q\n", baseFile.CorpusSHA256, corpusSHA)
			return 1
		}
		if baseFile.TaskInstructionSHA256 != primaryTaskSHA {
			_, _ = fmt.Fprintf(stderr, "detonate: --from-results task_instruction_sha256 %q != current task %q\n", baseFile.TaskInstructionSHA256, primaryTaskSHA)
			return 1
		}
		opts.Datamark = baseFile.Stages.Datamark
		opts.Framing = baseFile.Stages.Framing
		*verifyFlag = baseFile.Stages.Verify
		mode = reader.Mode(*verifyFlag)
		if mode == reader.ModeSchema && len(tasks) > 0 {
			primarySchemaSHA = schemaSHAByTask[tasks[0].ID]
		} else {
			primarySchemaSHA = ""
		}
		if baseFile.SchemaSHA256 != primarySchemaSHA {
			_, _ = fmt.Fprintf(stderr, "detonate: --from-results schema_sha256 %q != current schema %q\n", baseFile.SchemaSHA256, primarySchemaSHA)
			return 1
		}
		evaluated = make([]report.RunResult, len(baseFile.Runs))
		for i, r := range baseFile.Runs {
			r.Audit = nil
			r.Outcomes.AuditFlagged = false
			evaluated[i] = r
		}
		framingSHA = baseFile.FramingSHA256
		modelName = baseFile.Model
		readerEndpointHost = baseFile.EndpointHost
		hwName = baseFile.Hardware
		outRankDelta = baseFile.RankDelta
		outTrials = baseFile.Trials
		if auditMeta != nil && auditMeta.Mode == "chat" && auditMeta.EndpointHost == "" {
			auditMeta.EndpointHost = readerEndpointHost
		}
	} else {
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

		// Job j is case j%len(cases) in trial j/len(cases)+1, so runs are written trial-major in corpus order.
		total := len(cases) * *trialsFlag
		rawRuns := make([]report.RunResult, total)
		var firstErr error
		var errOnce sync.Once

		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		jobs := make(chan int, total)
		for j := 0; j < total; j++ {
			jobs <- j
		}
		close(jobs)

		var wg sync.WaitGroup
		workers := *concurrencyFlag
		if workers > total && total > 0 {
			workers = total
		}
		var fshaOnce sync.Once
		var doneCount int64
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					if runCtx.Err() != nil {
						return
					}
					c := cases[idx%len(cases)]
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
					rr.Trial = idx/len(cases) + 1
					rawRuns[idx] = rr
					n := atomic.AddInt64(&doneCount, 1)
					if n%20 == 0 || int(n) == total {
						_, _ = fmt.Fprintf(stderr, "detonate: progress %d/%d (%s, %dms)\n", n, total, c.ID, rr.DurationMs)
					}
				}
			}()
		}
		wg.Wait()

		if firstErr != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: %v\n", firstErr)
			return 1
		}

		evaluated, err = report.Evaluate(cases, rawRuns, *verifyFlag, *rankDeltaFlag)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: evaluate: %v\n", err)
			return 1
		}
	}

	if auditor != nil {
		if err := executeAudit(ctx, auditor, *auditFlag, *verifyFlag, cases, taskByID, evaluated, *concurrencyFlag, *timeoutFlag); err != nil {
			_, _ = fmt.Fprintf(stderr, "detonate: %v\n", err)
			return 1
		}
	}

	var stageAudit string
	if *auditFlag != "off" {
		stageAudit = *auditFlag
	}

	resFile := report.File{
		Version:               1,
		Model:                 modelName,
		EndpointHost:          readerEndpointHost,
		Hardware:              hwName,
		CorpusSHA256:          corpusSHA,
		TaskInstructionSHA256: primaryTaskSHA,
		FramingSHA256:         framingSHA,
		SchemaSHA256:          primarySchemaSHA,
		Stages: report.StageConfig{
			Datamark: opts.Datamark,
			Framing:  opts.Framing,
			Verify:   *verifyFlag,
			Audit:    stageAudit,
		},
		AuditMeta: auditMeta,
		RankDelta: outRankDelta,
		Trials:    outTrials,
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

func buildAuditor(
	auditMode string,
	semselectURL string,
	semselectKey string,
	semselectRev string,
	semselectModel string,
	auditChatModel string,
	threshold float64,
	readerEndpointHost string,
	readerClient *reader.Client,
) (audit.Auditor, *report.AuditMeta, error) {
	switch auditMode {
	case "off", "":
		return nil, nil, nil
	case "semselect":
		if strings.TrimSpace(semselectURL) == "" {
			return nil, nil, errors.New("--semselect-url (or SEMGUARD_SEMSELECT_URL) is required when --audit=semselect")
		}
		if threshold <= 0 || threshold > 1 {
			return nil, nil, fmt.Errorf("--audit-threshold must be in (0, 1], got %v", threshold)
		}
		host, err := sanitizeEndpointHost(semselectURL)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid --semselect-url: %w", err)
		}
		return &audit.SemselectAuditor{
				BaseURL:    semselectURL,
				APIKey:     semselectKey,
				HTTPClient: &http.Client{},
				Threshold:  threshold,
			}, &report.AuditMeta{
				Mode:              "semselect",
				EndpointHost:      host,
				SemselectRevision: strings.TrimSpace(semselectRev),
				SemselectModel:    strings.TrimSpace(semselectModel),
				Threshold:         threshold,
			}, nil
	case "chat":
		if strings.TrimSpace(auditChatModel) == "" {
			return nil, nil, errors.New("--audit-chat-model (or SEMGUARD_AUDIT_CHAT_MODEL) is required when --audit=chat")
		}
		return &audit.ChatAuditor{
				Client: readerClient,
				Model:  strings.TrimSpace(auditChatModel),
			}, &report.AuditMeta{
				Mode:         "chat",
				EndpointHost: readerEndpointHost,
				Model:        strings.TrimSpace(auditChatModel),
			}, nil
	default:
		return nil, nil, fmt.Errorf("unknown --audit mode %q", auditMode)
	}
}

func executeAudit(
	ctx context.Context,
	auditor audit.Auditor,
	auditMode string,
	verifyMode string,
	cases []corpus.Case,
	taskByID map[string]corpus.Task,
	runs []report.RunResult,
	concurrency int,
	timeout time.Duration,
) error {
	caseByID := make(map[string]corpus.Case, len(cases))
	for _, c := range cases {
		caseByID[c.ID] = c
	}

	auditCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, len(runs))
	for i := range runs {
		if runs[i].Record != nil && !runs[i].Refused && !runs[i].Outcomes.B1 {
			jobs <- i
		}
	}
	close(jobs)

	workers := concurrency
	if auditMode == "semselect" {
		// semselect serves a single inference slot and returns 429 when busy;
		// run semselect audit probes serially regardless of --concurrency.
		workers = 1
	}
	if workers > len(runs) && len(runs) > 0 {
		workers = len(runs)
	}
	if workers < 1 {
		workers = 1
	}

	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if auditCtx.Err() != nil {
					return
				}
				r := &runs[idx]
				c := caseByID[r.CaseID]
				t := taskByID[c.Task]
				reqCtx, reqCancel := context.WithTimeout(auditCtx, timeout)
				var rawFallback []string
				if verifyMode == string(reader.ModeFree) && len(r.Record.Claims) == 0 {
					rawFallback = []string{r.RawResponse}
				}
				res, err := auditor.Audit(reqCtx, untrusted.Task{
					Instruction: t.Instruction,
					Question:    c.Question,
				}, *r.Record, rawFallback...)
				reqCancel()
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("audit case %s: %w", r.CaseID, err)
						cancel()
					})
					return
				}
				r.Audit = &res
				if res.Label == audit.LabelFlag {
					r.Outcomes.AuditFlagged = true
				}
			}
		}()
	}
	wg.Wait()
	return firstErr
}
