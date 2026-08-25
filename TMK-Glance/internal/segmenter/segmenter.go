package segmenter

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ReasonPartial       = "partial"
	ReasonPunctuation   = "punctuation"
	ReasonMaxLength     = "max_length"
	ReasonMaxDuration   = "max_duration"
	ReasonPause         = "pause"
	ReasonSemantic      = "semantic"
	ReasonFallback      = "fallback"
	ReasonProviderFinal = "provider_final"
	ReasonSoftCommit    = "soft_commit"
	ReasonFlush         = "flush"
)

type Config struct {
	Enabled             bool
	MaxRunes            int
	MaxDuration         time.Duration
	SoftCommitDelay     time.Duration
	MinRunes            int
	MinDuration         time.Duration
	StableRevisionCount int
	PunctuationEnabled  bool
	SemanticEnabled     bool
	SemanticTimeout     time.Duration
	SemanticProvider    string
	BoundaryPolicy      string
}

type Input struct {
	Text        string
	IsFinal     bool
	BeginTimeMS int64
	EndTimeMS   int64
	Confidence  float64
	Provider    string
}

type Segment struct {
	ID            int64   `json:"id"`
	Revision      int64   `json:"revision"`
	Text          string  `json:"text"`
	IsFinal       bool    `json:"is_final"`
	Reason        string  `json:"reason"`
	BeginTimeMS   int64   `json:"begin_time_ms,omitempty"`
	EndTimeMS     int64   `json:"end_time_ms,omitempty"`
	BoundaryScore float64 `json:"boundary_score,omitempty"`
	Strategy      string  `json:"strategy,omitempty"`
	Stability     string  `json:"stability,omitempty"`
}

type SemanticInput struct {
	Language      string
	Text          string
	CandidateCuts []int
	PreviousText  string
	MaxRunes      int
}

type SemanticDecision struct {
	Boundaries []int
	Confidence float64
	Reason     string
}

// SemanticSegmenter is intentionally caller-owned. Real-time callers may run it
// asynchronously; the core state machine never waits on a provider.
type SemanticSegmenter interface {
	Suggest(context.Context, SemanticInput) (SemanticDecision, error)
}

// ApplySemanticDecision applies a decision produced asynchronously by a
// SemanticSegmenter. Callers must serialize it with Push/Tick/Flush and may
// discard decisions whose input revision is no longer current.
func (s *Segmenter) ApplySemanticDecision(decision SemanticDecision, now time.Time) []Segment {
	combined := []rune(joinText(s.carry, s.provider))
	if s.consumed >= len(combined) || len(decision.Boundaries) == 0 {
		return nil
	}
	boundaries := append([]int(nil), decision.Boundaries...)
	sort.Ints(boundaries)
	output := make([]Segment, 0, len(boundaries))
	start := s.consumed
	for _, cut := range boundaries {
		if cut <= start || cut > len(combined) || cut-start < s.config.MinRunes {
			continue
		}
		text := strings.TrimSpace(string(combined[start:cut]))
		if text == "" {
			continue
		}
		score := decision.Confidence
		if score <= 0 {
			score = 0.70
		}
		if score > 1 {
			score = 1
		}
		output = append(output, s.emitFinal(text, ReasonSemantic, now, score, "semantic"))
		start = cut
	}
	s.consumed = start
	if s.consumed >= len(combined) {
		s.resetProviderState()
	}
	return output
}

type pendingSegment struct {
	text      string
	deadline  time.Time
	startedAt time.Time
}

// Segmenter converts a provider's cumulative hypotheses into bounded,
// revisioned segments. It is a synchronous state machine; callers own timing.
type Segmenter struct {
	config Config

	segmentID   int64
	revision    int64
	carry       string
	provider    string
	previous    string
	consumed    int
	lastText    string
	startedAt   time.Time
	beginTimeMS int64
	endTimeMS   int64
	pending     *pendingSegment
}

func New(config Config) *Segmenter {
	if config.MaxRunes <= 0 {
		config.MaxRunes = 40
	}
	if config.MaxDuration <= 0 {
		config.MaxDuration = 5 * time.Second
	}
	if config.SoftCommitDelay <= 0 {
		config.SoftCommitDelay = 300 * time.Millisecond
	}
	if config.MinRunes <= 0 {
		config.MinRunes = 1
	}
	if config.MinDuration < 0 {
		config.MinDuration = 0
	}
	if config.SemanticTimeout <= 0 {
		config.SemanticTimeout = 200 * time.Millisecond
	}
	if config.BoundaryPolicy == "" {
		config.BoundaryPolicy = "balanced"
	}
	if config.PunctuationEnabled == false {
		config.PunctuationEnabled = true
	}
	return &Segmenter{config: config, segmentID: 1}
}

func (s *Segmenter) Push(input Input, now time.Time) []Segment {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		if input.IsFinal {
			return s.Tick(now)
		}
		return nil
	}

	var output []Segment
	if s.pending != nil {
		if shouldContinue(s.pending.text) {
			s.carry = s.pending.text
			s.startedAt = s.pending.startedAt
			s.lastText = s.pending.text
			s.pending = nil
		} else {
			output = append(output, s.commitPending(ReasonSoftCommit)...)
		}
	}

	s.provider = text
	if input.BeginTimeMS > 0 && s.beginTimeMS == 0 {
		s.beginTimeMS = input.BeginTimeMS
	}
	if input.EndTimeMS > s.endTimeMS {
		s.endTimeMS = input.EndTimeMS
	}
	combined := joinText(s.carry, s.provider)
	combinedRunes := []rune(combined)
	if s.consumed > len(combinedRunes) {
		s.consumed = len(combinedRunes)
	}
	if s.startedAt.IsZero() && s.consumed < len(combinedRunes) {
		s.startedAt = now
	}

	stableLimit := commonPrefixRunes(s.previous, combined)
	if input.IsFinal {
		stableLimit = len(combinedRunes)
	}

	for s.consumed < len(combinedRunes) {
		remainder := combinedRunes[s.consumed:]
		stableWindow := stableLimit - s.consumed
		if stableWindow > s.config.MaxRunes {
			stableWindow = s.config.MaxRunes
		}
		if s.config.PunctuationEnabled {
			if boundary := punctuationBoundary(remainder, stableWindow); boundary > 0 && s.boundaryAllowed(boundary) {
				output = append(output, s.emitFinal(string(remainder[:boundary]), ReasonPunctuation, now, 0.95, "rule"))
				s.consumed += boundary
				continue
			}
		}
		if len(remainder) > s.config.MaxRunes {
			cut := preferredBoundary(remainder, s.config.MaxRunes)
			output = append(output, s.emitFinal(string(remainder[:cut]), ReasonMaxLength, now, 0.35, "fallback"))
			s.consumed += cut
			continue
		}
		if s.durationExceeded(input, now) {
			output = append(output, s.emitFinal(string(remainder), ReasonMaxDuration, now, 0.35, "fallback"))
			s.consumed = len(combinedRunes)
			continue
		}
		break
	}

	if s.consumed < len(combinedRunes) {
		remainder := strings.TrimSpace(string(combinedRunes[s.consumed:]))
		if input.IsFinal {
			if hasTerminalPunctuation(remainder) {
				output = append(output, s.emitFinal(remainder, ReasonProviderFinal, now, 0.90, "rule"))
				s.consumed = len(combinedRunes)
			} else {
				output = append(output, s.emitPartial(remainder)...)
				s.pending = &pendingSegment{
					text:      remainder,
					deadline:  now.Add(s.config.SoftCommitDelay),
					startedAt: s.startedAt,
				}
			}
		} else {
			output = append(output, s.emitPartial(remainder)...)
		}
	}

	s.previous = combined
	if input.IsFinal {
		s.resetProviderState()
	}
	return output
}

func (s *Segmenter) Tick(now time.Time) []Segment {
	if s.pending == nil || now.Before(s.pending.deadline) {
		return nil
	}
	return s.commitPending(ReasonSoftCommit)
}

func (s *Segmenter) Flush(now time.Time) []Segment {
	if s.pending != nil {
		return s.commitPending(ReasonFlush)
	}
	combined := []rune(joinText(s.carry, s.provider))
	if s.consumed >= len(combined) {
		return nil
	}
	text := strings.TrimSpace(string(combined[s.consumed:]))
	if text == "" {
		return nil
	}
	return []Segment{s.emitFinal(text, ReasonFlush, now, 0.20, "fallback")}
}

func (s *Segmenter) emitPartial(text string) []Segment {
	text = strings.TrimSpace(text)
	if text == "" || text == s.lastText {
		return nil
	}
	s.revision++
	s.lastText = text
	return []Segment{{ID: s.segmentID, Revision: s.revision, Text: text, Reason: ReasonPartial, BeginTimeMS: s.beginTimeMS, EndTimeMS: s.endTimeMS, Stability: "partial", Strategy: "rule"}}
}

func (s *Segmenter) emitFinal(text, reason string, now time.Time, score float64, strategy string) Segment {
	text = strings.TrimSpace(text)
	s.revision++
	result := Segment{ID: s.segmentID, Revision: s.revision, Text: text, IsFinal: true, Reason: reason, BeginTimeMS: s.beginTimeMS, EndTimeMS: s.endTimeMS, BoundaryScore: score, Strategy: strategy, Stability: "final"}
	s.segmentID++
	s.revision = 0
	s.lastText = ""
	s.startedAt = now
	s.beginTimeMS, s.endTimeMS = 0, 0
	return result
}

func (s *Segmenter) commitPending(reason string) []Segment {
	if s.pending == nil {
		return nil
	}
	text := s.pending.text
	s.pending = nil
	result := s.emitFinal(text, reason, time.Time{}, 0.60, "rule")
	s.resetProviderState()
	s.startedAt = time.Time{}
	return []Segment{result}
}

func (s *Segmenter) boundaryAllowed(cut int) bool {
	return cut >= s.config.MinRunes && (s.config.MinDuration == 0 || s.endTimeMS-s.beginTimeMS >= s.config.MinDuration.Milliseconds())
}

func (s *Segmenter) resetProviderState() {
	s.carry = ""
	s.provider = ""
	s.previous = ""
	s.consumed = 0
	if s.pending == nil {
		s.startedAt = time.Time{}
	}
}

func (s *Segmenter) durationExceeded(input Input, now time.Time) bool {
	if input.EndTimeMS > input.BeginTimeMS && time.Duration(input.EndTimeMS-input.BeginTimeMS)*time.Millisecond >= s.config.MaxDuration {
		return true
	}
	return !s.startedAt.IsZero() && now.Sub(s.startedAt) >= s.config.MaxDuration
}

func commonPrefixRunes(left, right string) int {
	a, b := []rune(left), []rune(right)
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

func punctuationBoundary(text []rune, stable int) int {
	if stable > len(text) {
		stable = len(text)
	}
	for i := 0; i < stable; i++ {
		if isTerminalPunctuation(text[i]) {
			return i + 1
		}
	}
	return 0
}

func preferredBoundary(text []rune, max int) int {
	if max >= len(text) {
		return len(text)
	}
	for i := max - 1; i >= max/2; i-- {
		if isTerminalPunctuation(text[i]) || unicode.IsSpace(text[i]) || text[i] == ',' || text[i] == '\uff0c' {
			return i + 1
		}
	}
	return max
}

func hasTerminalPunctuation(text string) bool {
	text = strings.TrimSpace(text)
	last, _ := utf8.DecodeLastRuneInString(text)
	return isTerminalPunctuation(last)
}

func isTerminalPunctuation(r rune) bool {
	switch r {
	case '.', '!', '?', ';', '\u3002', '\uff01', '\uff1f', '\uff1b':
		return true
	default:
		return false
	}
}

func shouldContinue(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" || hasTerminalPunctuation(text) {
		return false
	}
	connectors := []string{
		"\u56e0\u4e3a", "\u6240\u4ee5", "\u4f46\u662f", "\u7136\u540e", "\u5982\u679c", "\u867d\u7136", "\u5e76\u4e14", "\u800c\u4e14", "\u53ef\u662f", "\u6216\u8005",
		" because", " so", " but", " and", " if", " although", " then", " or",
	}
	for _, suffix := range connectors {
		if strings.HasSuffix(text, suffix) || text == strings.TrimSpace(suffix) {
			return true
		}
	}
	return false
}

func joinText(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	l, _ := utf8.DecodeLastRuneInString(left)
	r, _ := utf8.DecodeRuneInString(right)
	if isASCIIWord(l) && isASCIIWord(r) {
		return left + " " + right
	}
	return left + right
}

func isASCIIWord(r rune) bool {
	return r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
