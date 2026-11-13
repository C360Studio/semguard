package intake

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// CheckName identifies one of the 8 deterministic passage intake checks.
type CheckName string

const (
	// CheckTalksToReader flags passages that address the reader/model or give instructions.
	CheckTalksToReader CheckName = "talks_to_reader"
	// CheckSelfRankClaim flags passages that claim to be authoritative, canonical, or top-ranked.
	CheckSelfRankClaim CheckName = "self_rank_claim"
	// CheckFakeMarkers flags passages containing fake role tags, chat delimiters, or XML/JSON prompt wrappers.
	CheckFakeMarkers CheckName = "fake_markers"
	// CheckEncodedBlob flags passages containing base64, hex, or URL-encoded runs above threshold.
	CheckEncodedBlob CheckName = "encoded_blob"
	// CheckUnlistedLink flags passages containing a URL not present in the result's outbound link list.
	CheckUnlistedLink CheckName = "unlisted_link"
	// CheckLoneSite flags passages citing an external site cited by no other report in the batch.
	CheckLoneSite CheckName = "lone_site"
	// CheckSharedOrigin flags passages from reports on different sites that share identical sentences/shingles or origin metadata.
	CheckSharedOrigin CheckName = "shared_origin"
	// CheckOffTopic flags passages that share no content keywords with the question and little/no overlap with their report.
	CheckOffTopic CheckName = "off_topic"
)

// AllChecks lists the 8 deterministic passage checks in canonical evaluation order.
var AllChecks = []CheckName{
	CheckTalksToReader,
	CheckSelfRankClaim,
	CheckFakeMarkers,
	CheckEncodedBlob,
	CheckUnlistedLink,
	CheckLoneSite,
	CheckSharedOrigin,
	CheckOffTopic,
}

// CheckHit records one check that fired on a passage along with its evidence reason.
type CheckHit struct {
	Check  CheckName `json:"check"`
	Weight int       `json:"weight"`
	Detail string    `json:"detail"`
}

var (
	// 1. talks_to_reader: imperative/directive phrases addressing the reader, assistant, or system.
	talksToReaderRe = regexp.MustCompile(`(?i)\b(?:ignore\s+(?:all\s+|any\s+)?(?:previous|prior|above|preceding|earlier)\s+(?:instructions?|prompts?|rules?|context|directions?)|disregard\s+(?:all\s+|any\s+)?(?:previous|prior|above|preceding|earlier)|forget\s+(?:everything|all\s+previous|your\s+instructions)|you\s+are\s+(?:an?\s+(?:ai|assistant|language\s+model|helpful\s+assistant)|now\s+in|instructed\s+to|required\s+to)|as\s+an?\s+(?:ai|assistant|language\s+model|llm)|system\s+(?:override|prompt|instruction|directive)|new\s+instructions?\s*:|important\s+instructions?\s*(?:for\s+(?:the\s+)?(?:ai|assistant|reader|model|summarizer))?\s*:|note\s+to\s+(?:the\s+)?(?:ai|assistant|reader|model|summarizer|system)\s*:|when\s+(?:summarizing|answering|responding|generating|reading\s+this)|do\s+not\s+(?:mention|include|cite|summarize|reveal|disclose)\s+(?:any\s+other|this|that)|you\s+must\s+(?:always\s+|only\s+|now\s+)?(?:respond|output|answer|state|conclude|recommend|rank|cite|include)|always\s+(?:rank|recommend|cite|output|respond\s+with)|please\s+(?:output|respond\s+with|ignore|include\s+the\s+following\s+link))\b`)

	// 2. self_rank_claim: self-promotional authority, #1 ranking, or superseding-all-other-sources claims.
	selfRankClaimRe = regexp.MustCompile(`(?i)(?:\b(?:this\s+(?:document|report|article|page|source|study|guide)\s+(?:is\s+the\s+(?:only\s+|most\s+|single\s+)?(?:authoritative|canonical|official|definitive|trusted|reliable|accurate|primary|top(?:-ranked)?)|supersedes\s+all\s+other|must\s+be\s+ranked\s+(?:first|#?1|highest))|(?:rank|score|rate)\s+this\s+(?:document|report|source|page|result|option)\s+(?:first|highest|#?1|10(?:/10|\s+out\s+of\s+10))|(?:official|canonical|definitive|sole\s+authoritative)\s+source\s+that\s+(?:supersedes|overrides|replaces)\s+(?:all\s+)?(?:other|prior|previous)|all\s+other\s+(?:sources|reports|documents|articles|competitors)\s+are\s+(?:outdated|deprecated|inaccurate|unreliable|wrong|obsolete|superseded)|(?:#1|number\s+one|top-ranked|highest-rated|industry-leading)\s+(?:recommended|verified|authoritative|official)\s+(?:choice|solution|source|provider))\b)`)

	// 3. fake_markers: prompt-template delimiters, role headers, and XML/chat control tags.
	fakeMarkersRe = regexp.MustCompile(`(?im)(?:<\s*/?\s*(?:system|user|assistant|instruction|instructions|prompt|context|tool_call|tool_response|function_call|document|report|source)\b[^>]*>|\[/?(?:INST|SYS|SYSTEM|USER|ASSISTANT)\]|<\|(?:im_start|im_end|system|user|assistant|endoftext)\|>|(?:^|\n)\s*(?:###\s*(?:System|Instruction|Instructions|Human|Assistant|User)\s*:|(?:SYSTEM|USER|ASSISTANT|INSTRUCTION)\s*:\s*\S)|\{\s*"(?:role|system_prompt|instructions)"\s*:\s*"(?:system|user|assistant|ignore))`)

	// 4. encoded_blob: long base64, hex, or URL-percent-encoded payloads.
	// Hex run of >= 32 hex chars (not part of a URL path), URL-encoded run of >= 8 %XX sequences,
	// or base64-like token of >= 40 chars containing mixed case/digits and+/= padding or density.
	hexBlobRe         = regexp.MustCompile(`\b(?:0x)?[0-9a-fA-F]{32,}\b`)
	urlEncodedBlobRe  = regexp.MustCompile(`(?:%[0-9a-fA-F]{2}){8,}`)
	base64CandidateRe = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

func checkTalksToReader(body string) (bool, string) {
	if m := talksToReaderRe.FindString(body); m != "" {
		return true, fmt.Sprintf("matched reader directive %q", truncateDetail(m))
	}
	return false, ""
}

func checkSelfRankClaim(body string) (bool, string) {
	if m := selfRankClaimRe.FindString(body); m != "" {
		return true, fmt.Sprintf("matched self-rank/authority claim %q", truncateDetail(m))
	}
	return false, ""
}

func checkFakeMarkers(body string) (bool, string) {
	if m := fakeMarkersRe.FindString(body); m != "" {
		return true, fmt.Sprintf("matched fake marker %q", truncateDetail(strings.TrimSpace(m)))
	}
	return false, ""
}

func checkEncodedBlob(body string) (bool, string) {
	// Strip URLs before checking for encoded blobs so long query parameters or hashes in legitimate URLs
	// do not false-positive unless the URL itself is heavily percent-encoded.
	if m := urlEncodedBlobRe.FindString(body); m != "" {
		return true, fmt.Sprintf("matched URL-encoded run %q", truncateDetail(m))
	}
	withoutURLs := rawURLRe.ReplaceAllString(body, " ")
	if m := hexBlobRe.FindString(withoutURLs); m != "" {
		return true, fmt.Sprintf("matched hex blob %q", truncateDetail(m))
	}
	for _, cand := range base64CandidateRe.FindAllString(withoutURLs, -1) {
		if looksLikeBase64(cand) {
			return true, fmt.Sprintf("matched base64 blob %q", truncateDetail(cand))
		}
	}
	return false, ""
}

func looksLikeBase64(s string) bool {
	if len(s)%4 != 0 && !strings.HasSuffix(s, "=") {
		return false
	}
	var hasUpper, hasLower, hasDigitOrSym bool
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=':
			hasDigitOrSym = true
		}
	}
	return hasUpper && hasLower && hasDigitOrSym
}

func checkUnlistedLink(body string, rep Report) (bool, string) {
	if !rep.LinksPresent {
		return false, ""
	}
	_, urlsInPassage := canonicalizeURLsInText(body)
	if len(urlsInPassage) == 0 {
		return false, ""
	}
	allowed := make(map[string]bool, len(rep.Links)+1)
	if rep.CanonicalURL != "" {
		allowed[rep.CanonicalURL] = true
	}
	for _, l := range rep.Links {
		if c, ok := CanonicalizeURL(l); ok {
			allowed[c] = true
		} else {
			allowed[strings.TrimSpace(l)] = true
		}
	}
	for _, u := range urlsInPassage {
		if !allowed[u] {
			return true, fmt.Sprintf("URL %q absent from result's link list", u)
		}
	}
	return false, ""
}

// batchContext precomputes cross-report statistics for lone_site, shared_origin, and off_topic.
type batchContext struct {
	questionTokens map[string]bool
	// siteReportCount maps a siteHost to the set of report indices that either come from or cite that site.
	siteReportCount map[string]map[int]bool
	// sentenceSites maps a normalized sentence (>= 60 chars) to the set of distinct report siteHosts containing it.
	sentenceSites map[string]map[string]bool
	// shingleSites maps an 8-word normalized shingle to the set of distinct report siteHosts containing it.
	shingleSites map[string]map[string]bool
	// metaOriginSites maps "author|publishedDate" (when both non-empty) to distinct report siteHosts.
	metaOriginSites map[string]map[string]bool
}

func buildBatchContext(question string, reports []Report) *batchContext {
	bc := &batchContext{
		questionTokens:  extractContentTokens(question),
		siteReportCount: make(map[string]map[int]bool),
		sentenceSites:   make(map[string]map[string]bool),
		shingleSites:    make(map[string]map[string]bool),
		metaOriginSites: make(map[string]map[string]bool),
	}

	recordSite := func(site string, repIdx int) {
		if site == "" {
			return
		}
		if bc.siteReportCount[site] == nil {
			bc.siteReportCount[site] = make(map[int]bool)
		}
		bc.siteReportCount[site][repIdx] = true
	}

	for repIdx, rep := range reports {
		repSite := rep.Site
		if repSite == "" {
			repSite = SiteHost(rep.CanonicalURL)
		}
		recordSite(repSite, repIdx)

		for _, l := range rep.Links {
			recordSite(SiteHost(l), repIdx)
		}
		for _, psg := range rep.Passages {
			_, urls := canonicalizeURLsInText(psg.Body)
			for _, u := range urls {
				recordSite(SiteHost(u), repIdx)
			}
			if repSite != "" {
				for _, sent := range extractNormalizedSentences(psg.Body) {
					if bc.sentenceSites[sent] == nil {
						bc.sentenceSites[sent] = make(map[string]bool)
					}
					bc.sentenceSites[sent][repSite] = true
				}
				for _, sh := range extractWordShingles(psg.Body, 8) {
					if bc.shingleSites[sh] == nil {
						bc.shingleSites[sh] = make(map[string]bool)
					}
					bc.shingleSites[sh][repSite] = true
				}
			}
		}

		if repSite != "" && rep.Author != nil && rep.PublishedDate != nil {
			author := strings.ToLower(strings.TrimSpace(*rep.Author))
			pub := strings.TrimSpace(*rep.PublishedDate)
			if author != "" && pub != "" {
				key := author + "|" + pub
				if bc.metaOriginSites[key] == nil {
					bc.metaOriginSites[key] = make(map[string]bool)
				}
				bc.metaOriginSites[key][repSite] = true
			}
		}
	}

	return bc
}

func checkLoneSite(body string, repIdx int, rep Report, totalReports int, bc *batchContext) (bool, string) {
	if totalReports < 2 {
		return false, ""
	}
	repSite := rep.Site
	if repSite == "" {
		repSite = SiteHost(rep.CanonicalURL)
	}
	_, urls := canonicalizeURLsInText(body)
	for _, u := range urls {
		citedSite := SiteHost(u)
		if citedSite == "" || citedSite == repSite {
			continue
		}
		reps := bc.siteReportCount[citedSite]
		if len(reps) <= 1 && reps[repIdx] {
			return true, fmt.Sprintf("cites site %q not referenced by any other report", citedSite)
		}
	}
	return false, ""
}

func checkSharedOrigin(body string, rep Report, bc *batchContext) (bool, string) {
	repSite := rep.Site
	if repSite == "" {
		repSite = SiteHost(rep.CanonicalURL)
	}
	if repSite == "" {
		return false, ""
	}
	for _, sent := range extractNormalizedSentences(body) {
		if sites := bc.sentenceSites[sent]; len(sites) >= 2 {
			return true, fmt.Sprintf("shares sentence %q across %d distinct sites", truncateDetail(sent), len(sites))
		}
	}
	for _, sh := range extractWordShingles(body, 8) {
		if sites := bc.shingleSites[sh]; len(sites) >= 2 {
			return true, fmt.Sprintf("shares 8-word shingle %q across %d distinct sites", truncateDetail(sh), len(sites))
		}
	}
	if rep.Author != nil && rep.PublishedDate != nil {
		author := strings.ToLower(strings.TrimSpace(*rep.Author))
		pub := strings.TrimSpace(*rep.PublishedDate)
		if author != "" && pub != "" {
			key := author + "|" + pub
			if sites := bc.metaOriginSites[key]; len(sites) >= 2 {
				return true, fmt.Sprintf("shares author/publishedDate metadata (%s) across %d distinct sites", key, len(sites))
			}
		}
	}
	return false, ""
}

func checkOffTopic(psgIdx int, body string, rep Report, bc *batchContext) (bool, string) {
	psgTokens := extractContentTokens(body)
	// Require enough content tokens in the passage to judge topicality reliably.
	if len(psgTokens) < 6 || len(bc.questionTokens) == 0 {
		return false, ""
	}
	for tok := range psgTokens {
		if bc.questionTokens[tok] {
			return false, ""
		}
	}

	// Zero overlap with question: also check overlap with sibling passages and report title.
	siblingTokens := extractContentTokens(rep.Title)
	for i, other := range rep.Passages {
		if i == psgIdx {
			continue
		}
		for tok := range extractContentTokens(other.Body) {
			siblingTokens[tok] = true
		}
	}
	if len(siblingTokens) == 0 {
		return true, "zero content-keyword overlap with question"
	}

	sharedWithReport := 0
	for tok := range psgTokens {
		if siblingTokens[tok] {
			sharedWithReport++
		}
	}
	if sharedWithReport == 0 {
		return true, "zero content-keyword overlap with both question and report"
	}
	union := len(psgTokens) + len(siblingTokens) - sharedWithReport
	if union > 0 && float64(sharedWithReport)/float64(union) < 0.05 {
		return true, fmt.Sprintf("zero question overlap and low report overlap (%d/%d tokens)", sharedWithReport, union)
	}
	return false, ""
}

var stopwords = map[string]bool{
	"about": true, "above": true, "after": true, "again": true, "against": true,
	"all": true, "also": true, "am": true, "an": true, "and": true, "any": true,
	"are": true, "as": true, "at": true, "be": true, "because": true, "been": true,
	"before": true, "being": true, "below": true, "between": true, "both": true,
	"but": true, "by": true, "can": true, "could": true, "did": true, "do": true,
	"does": true, "doing": true, "down": true, "during": true, "each": true,
	"few": true, "for": true, "from": true, "further": true, "had": true,
	"has": true, "have": true, "having": true, "he": true, "her": true, "here": true,
	"hers": true, "herself": true, "him": true, "himself": true, "his": true,
	"how": true, "however": true, "http": true, "https": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "itself": true, "just": true,
	"like": true, "may": true, "me": true, "might": true, "more": true, "most": true,
	"must": true, "my": true, "myself": true, "no": true, "nor": true, "not": true,
	"now": true, "of": true, "off": true, "on": true, "once": true, "only": true,
	"or": true, "other": true, "our": true, "ours": true, "ourselves": true,
	"out": true, "over": true, "own": true, "same": true, "she": true, "should": true,
	"so": true, "some": true, "such": true, "than": true, "that": true, "the": true,
	"their": true, "theirs": true, "them": true, "themselves": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "those": true,
	"through": true, "to": true, "too": true, "under": true, "until": true,
	"up": true, "very": true, "was": true, "we": true, "were": true, "what": true,
	"when": true, "where": true, "which": true, "while": true, "who": true,
	"whom": true, "why": true, "will": true, "with": true, "would": true,
	"www": true, "you": true, "your": true, "yours": true, "yourself": true,
}

func extractContentTokens(s string) map[string]bool {
	withoutURLs := rawURLRe.ReplaceAllString(s, " ")
	tokens := make(map[string]bool)
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 3 {
			w := cur.String()
			if !stopwords[w] {
				tokens[w] = true
			}
		}
		cur.Reset()
	}
	for _, r := range strings.ToLower(withoutURLs) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func extractNormalizedSentences(s string) []string {
	var out []string
	for _, raw := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	}) {
		norm := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
		if len(norm) >= 60 {
			out = append(out, norm)
		}
	}
	return out
}

func extractWordShingles(s string, k int) []string {
	var words []string
	var cur strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	if len(words) < k {
		return nil
	}
	out := make([]string, 0, len(words)-k+1)
	for i := 0; i <= len(words)-k; i++ {
		out = append(out, strings.Join(words[i:i+k], " "))
	}
	return out
}

func truncateDetail(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:77] + "..."
}
