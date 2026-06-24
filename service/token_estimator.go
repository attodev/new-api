package service

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// Provider
type Provider string

const (
	OpenAI  Provider = "openai"  // GPT-3.5, GPT-4, GPT-4o
	Gemini  Provider = "gemini"  // Gemini 1.0, 1.5 Pro/Flash
	Claude  Provider = "claude"  // Claude 3, 3.5 Sonnet
	Unknown Provider = "unknown"
)

// multipliers
type multipliers struct {
	Word       float64 // ()
	Number     float64 // ()
	CJK        float64 // ()
	Symbol     float64 // ()
	MathSymbol float64 // (∑,∫,∂,√)
	URLDelim   float64 // URL (/,:,?,&,=,#,%) - tokenizer
	AtSign     float64 // @ -
	Emoji      float64 // Emoji ()
	Newline    float64 // / ()
	Space      float64 // ()
	BasePad    int     // (Start/End tokens)
}

var (
	multipliersMap = map[Provider]multipliers{
		Gemini: {
			Word: 1.15, Number: 2.8, CJK: 0.68, Symbol: 0.38, MathSymbol: 1.05, URLDelim: 1.2, AtSign: 2.5, Emoji: 1.08, Newline: 1.15, Space: 0.2, BasePad: 0,
		},
		Claude: {
			Word: 1.13, Number: 1.63, CJK: 1.21, Symbol: 0.4, MathSymbol: 4.52, URLDelim: 1.26, AtSign: 2.82, Emoji: 2.6, Newline: 0.89, Space: 0.39, BasePad: 0,
		},
		OpenAI: {
			Word: 1.02, Number: 1.55, CJK: 0.85, Symbol: 0.4, MathSymbol: 2.68, URLDelim: 1.0, AtSign: 2.0, Emoji: 2.12, Newline: 0.5, Space: 0.42, BasePad: 0,
		},
	}
	multipliersLock sync.RWMutex
)

// getMultipliers
func getMultipliers(p Provider) multipliers {
	multipliersLock.RLock()
	defer multipliersLock.RUnlock()

	switch p {
	case Gemini:
		return multipliersMap[Gemini]
	case Claude:
		return multipliersMap[Claude]
	case OpenAI:
		return multipliersMap[OpenAI]
	default:
		// ( OpenAI )
		return multipliersMap[OpenAI]
	}
}

// EstimateToken Token
func EstimateToken(provider Provider, text string) int {
	m := getMultipliers(provider)
	var count float64

	type WordType int
	const (
		None WordType = iota
		Latin
		Number
	)
	currentWordType := None

	for _, r := range text {
		// 1.
		if unicode.IsSpace(r) {
			currentWordType = None
			// Newline
			if r == '\n' || r == '\t' {
				count += m.Newline
			} else {
				// Space
				count += m.Space
			}
			continue
		}

		// 2. CJK () -
		if isCJK(r) {
			currentWordType = None
			count += m.CJK
			continue
		}

		// 3. Emoji - Emoji
		if isEmoji(r) {
			currentWordType = None
			count += m.Emoji
			continue
		}

		// 4. / ()
		if isLatinOrNumber(r) {
			isNum := unicode.IsNumber(r)
			newType := Latin
			if isNum {
				newType = Number
			}

			// <->token
			// OpenAI"version 3.5""abc123xyz"
			if currentWordType == None || currentWordType != newType {
				if newType == Number {
					count += m.Number
				} else {
					count += m.Word
				}
				currentWordType = newType
			}
			continue
		}

		// 5. / -
		currentWordType = None
		if isMathSymbol(r) {
			count += m.MathSymbol
		} else if r == '@' {
			count += m.AtSign
		} else if isURLDelim(r) {
			count += m.URLDelim
		} else {
			count += m.Symbol
		}
	}

	// padding
	return int(math.Ceil(count)) + m.BasePad
}

// CJK
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7A3)
}

func isLatinOrNumber(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// Emoji
func isEmoji(r rune) bool {
	// EmojiUnicode
	// 0x1F300-0x1F9FF (Emoticons, Symbols, Pictographs)
	// 0x2600-0x26FF (Misc Symbols), 0x2700-0x27BF (Dingbats)
	// 0x1F600-0x1F64F (Emoticons)
	// 0x1F900-0x1F9FF (Supplemental Symbols and Pictographs)
	return (r >= 0x1F300 && r <= 0x1F9FF) ||
		(r >= 0x2600 && r <= 0x26FF) ||
		(r >= 0x2700 && r <= 0x27BF) ||
		(r >= 0x1F600 && r <= 0x1F64F) ||
		(r >= 0x1F900 && r <= 0x1F9FF) ||
		(r >= 0x1FA00 && r <= 0x1FAFF) // Symbols and Pictographs Extended-A
}

func isMathSymbol(r rune) bool {
	// ∑ ∫ ∂ √ ∞ ≤ ≥ ≠ ≈ ± × ÷
	// ² ³ ¹ ⁴ ⁵ ⁶ ⁷ ⁸ ⁹ ⁰
	mathSymbols := "∑∫∂√∞≤≥≠≈±×÷∈∉∋∌⊂⊃⊆⊇∪∩∧∨¬∀∃∄∅∆∇∝∟∠∡∢°′″‴⁺⁻⁼⁽⁾ⁿ₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎²³¹⁴⁵⁶⁷⁸⁹⁰"
	for _, m := range mathSymbols {
		if r == m {
			return true
		}
	}
	// Mathematical Operators (U+2200–U+22FF)
	if r >= 0x2200 && r <= 0x22FF {
		return true
	}
	// Supplemental Mathematical Operators (U+2A00–U+2AFF)
	if r >= 0x2A00 && r <= 0x2AFF {
		return true
	}
	// Mathematical Alphanumeric Symbols (U+1D400–U+1D7FF)
	if r >= 0x1D400 && r <= 0x1D7FF {
		return true
	}
	return false
}

// URLtokenizer
func isURLDelim(r rune) bool {
	// URLtokenizer
	urlDelims := "/:?&=;#%"
	for _, d := range urlDelims {
		if r == d {
			return true
		}
	}
	return false
}

func EstimateTokenByModel(model, text string) int {
	// strings.Contains(model, "gpt-4o")
	if text == "" {
		return 0
	}

	model = strings.ToLower(model)
	if strings.Contains(model, "gemini") {
		return EstimateToken(Gemini, text)
	} else if strings.Contains(model, "claude") {
		return EstimateToken(Claude, text)
	} else {
		return EstimateToken(OpenAI, text)
	}
}
