// Package config holds every knob you are likely to turn.
// Renaming the publication or retuning the score thresholds
// should never require touching any other file.
package config

// ---------------------------------------------------------------------------
// Brand
// ---------------------------------------------------------------------------

const (
	// Name appears in the masthead and the <title>.
	Name = "SIGINT"

	// Mark is the small glyph beside the masthead. ^C sends SIGINT.
	Mark = "^C"

	// Standfirst sits under the masthead. Say what the page does, plainly.
	Standfirst = "Security research and practitioner news, scored for novelty and actionability."

	// Colophon is the small print in the footer. The first line makes the
	// .wtf deliberate — the reader is in on the joke, not wondering if the
	// domain was an accident. The second line keeps the honesty the rest of
	// the publication runs on. Rendered as two lines by the front end.
	Colophon = "sigint.wtf — the correct response to most of these CVEs. " +
		"Scored by machine against a hand-written rubric; ranking is opinion, not fact."
)

// ---------------------------------------------------------------------------
// Sources
// ---------------------------------------------------------------------------

// ArxivCategories are fetched in separate requests, 3s apart, as arXiv asks.
var ArxivCategories = []string{
	"cs.CR", // cryptography and security
	// "quant-ph", // quantum physics; noisy, so the rubric has to work harder
}

// ArxivMaxResults per category per run.
const ArxivMaxResults = 100

// KEVFeed is CISA's catalog of vulnerabilities confirmed exploited in the
// wild. Unauthenticated, returns the whole catalog every request, updated on
// US Eastern business days. The GitHub mirror below stays in sync within
// minutes and is the fallback if cisa.gov blocks the runner's IP — which has
// happened to CI systems before.
const (
	KEVFeed   = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	KEVMirror = "https://raw.githubusercontent.com/cisagov/kev-data/main/known_exploited_vulnerabilities.json"

	// KEVLookbackDays bounds how far back a KEV addition counts as news.
	// Without this, the first run ingests the entire historical catalog —
	// well over a thousand entries — and buries the front page.
	KEVLookbackDays = 14
)

// HNMinPoints filters out the long tail before we spend anything on scoring.
const HNMinPoints = 30

// HNLookbackHours bounds the Algolia query window.
const HNLookbackHours = 48

// HNKeywords is a crude title filter. Crude is correct here: the model does
// the real judging, and this only exists to keep the bill down. Lowercase.
//
// Widened after quant-ph was dropped left HN as the main "recent" source.
// The additions catch the vocabulary real security threads use that the
// original list missed: named technologies (openssl, kubernetes, passkey),
// incident language (breach, disclosure, postmortem), and the softer signals
// (outage, patch) that often front the day's actual security story. "qubit"
// was removed — pure physics with no security bearing.
var HNKeywords = []string{
	// core security
	"security", "vulnerability", "vulnerabilities", "exploit", "exploited",
	"cve", "breach", "ransomware", "malware", "backdoor", "supply chain",
	"spyware", "phishing", "credential", "data leak", "leaked", "compromise",
	// appsec + tooling
	"appsec", "sast", "dast", "sca", "fuzzing", "fuzzer", "sandbox",
	"sandboxing", "hardening", "threat model", "secure by design", "sbom",
	// identity + crypto
	"authentication", "authorization", "oauth", "oidc", "saml", "passkey",
	"passwordless", "mfa", "tls", "ssl", "openssl", "encryption",
	"cryptography", "crypto", "post-quantum", "signing", "certificate",
	"zero trust", "least privilege",
	// classes of bug
	"zero-day", "zero day", "rce", "xss", "sql injection", "ssrf", "csrf",
	"privilege escalation", "sandbox escape", "path traversal", "deserialization",
	"buffer overflow", "use-after-free", "memory safety",
	// AI security
	"prompt injection", "jailbreak", "llm security", "ai safety",
	"model poisoning", "adversarial", "agent security",
	// platform / infra that fronts security stories
	"kubernetes", "container escape", "cloud security", "iam", "secrets",
	"cve-", "patch tuesday", "disclosure", "advisory", "incident",
	"postmortem", "post-mortem", "outage", "compromised",
}

// ---------------------------------------------------------------------------
// Scoring
// ---------------------------------------------------------------------------

const (
	// Model is the scoring model. Haiku is plenty for this and costs cents.
	Model = "claude-haiku-4-5-20251001"

	// BatchSize is how many items go into one API call.
	BatchSize = 10

	// MaxScorePerRun hard-caps how many items get scored in one run, no matter
	// how many look fresh. Steady state scores 5-20, so 40 never bites in
	// normal operation. It exists solely to contain the one realistic runaway:
	// a dedup bug that makes the whole archive look new and quietly bills you
	// for 300 items. The Console spend cap is the backstop; this is the fuse.
	MaxScorePerRun = 40

	// MaxTokens per scoring call. Ten items of JSON fits comfortably.
	MaxTokens = 2000
)

// AxisHigh is the point at which an axis counts as "high". Everything at or
// above this is high; everything below is low.
//
// Tune this first if the quadrants come out lopsided. The rubric tells the
// model most items are 2s and 3s, so 4 keeps BREAK GLASS genuinely rare —
// which is the point. If NOISE FLOOR swallows everything, drop it to 3.
const AxisHigh = 4

// The four quadrants. A ladder collapses two independent judgements back into
// one number and throws away the interesting half: a Fortinet advisory is
// novelty 2, actionability 5 — important and not remotely new. No rung on a
// ladder can say that. STANDING ORDERS can.
const (
	QuadBreakGlass     = "BREAK GLASS"     // new and urgent
	QuadNewGround      = "NEW GROUND"      // new, nothing to do yet
	QuadStandingOrders = "STANDING ORDERS" // nothing new, act anyway
	QuadNoiseFloor     = "NOISE FLOOR"     // logged, not urgent
)

// Quadrants in display order, strongest first.
var Quadrants = []string{
	QuadBreakGlass, QuadStandingOrders, QuadNewGround, QuadNoiseFloor,
}

// Quadrant maps the two axes onto one of the four. Computed in Go, never asked
// of the model, so retuning AxisHigh costs nothing — see -requadrant.
func Quadrant(novelty, actionability int) string {
	newIdea := novelty >= AxisHigh
	urgent := actionability >= AxisHigh

	switch {
	case newIdea && urgent:
		return QuadBreakGlass
	case newIdea:
		return QuadNewGround
	case urgent:
		return QuadStandingOrders
	default:
		return QuadNoiseFloor
	}
}

// KEV scores are hard-assigned, never modelled. "Actively exploited" is a
// confirmed fact, not a judgement call, so there is nothing to score and no
// reason to pay for it.
//
// Novelty 3, not 5: the vulnerability is not new. What is new is the
// confirmation of exploitation. That lands the item in STANDING ORDERS —
// nothing new, act anyway — which is exactly right, and is the distinction a
// four-rung ladder could never express.
const (
	KEVNovelty = 3
	KEVAction  = 5

	// Ransomware use escalates it into BREAK GLASS.
	KEVNoveltyRansomware = 4
)

// ---------------------------------------------------------------------------
// Edition
// ---------------------------------------------------------------------------

const (
	// EditionWindowHours is how far back the front page reaches. 72h (three
	// days) suits a page that updates continuously and keeps it from looking
	// sparse on quiet days, without reaching so far back it stops feeling like
	// news.
	EditionWindowHours = 72

	// EditionMaxItems caps the front page. Scarcity is the product.
	// tdd.cat runs ~143 stories a day; that is a firehose wearing a
	// curation costume. Stay under 30.
	EditionMaxItems = 25

	// ArchiveRetentionDays bounds data/scored.json so it cannot grow forever.
	ArchiveRetentionDays = 30

	// NoiseCount is the size of the "Filed under noise" footer: the
	// highest-traffic stories of the day that scored lowest. Publicly
	// downgrading what everyone else is excited about is the whole point,
	// so keep it short and keep it visible.
	NoiseCount = 3
)

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

const (
	ArchivePath = "data/scored.json"
	EditionPath = "docs/edition.json"
)
