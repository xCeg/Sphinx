// Package aitext recognises text written for an AI model rather than for a
// person: the phrasing of prompt injections found hidden in papers and CVs.
package aitext

import (
	"regexp"
	"strings"
)

// patterns are deliberately specific. A hidden paragraph that merely
// mentions AI is not an instruction; "ignore previous instructions" is.
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ignore|disregard|forget)\b.{0,20}\b(previous|prior|above|earlier|all)\b.{0,20}\b(instructions?|prompts?|directions|rules)\b`),
	regexp.MustCompile(`(?i)\bnew instructions?\s*:`),
	regexp.MustCompile(`(?i)\b(you are|act as|as) (an? )?(ai|llm|large language model|language model|chatbot|assistant|reviewer)\b`),
	regexp.MustCompile(`(?i)\b(chatgpt|gpt-?[345]|openai|claude|gemini|copilot|llama|mistral|llm)s?\b.{0,40}\b(must|should|will|please|reviewer|reviewing|evaluat|rank|rate|score)`),
	regexp.MustCompile(`(?i)\b(give|write|provide)\b.{0,20}\bpositive (review|evaluation|assessment|feedback)\b`),
	regexp.MustCompile(`(?i)\bdo not (highlight|mention|point out|report)\b.{0,20}\b(negatives?|weakness(es)?|flaws?|errors?|limitations?)\b`),
	regexp.MustCompile(`(?i)\b(recommend|rank|rate|score)\b.{0,30}\b(this|the) (candidate|applicant|paper|submission|resume|cv)\b.{0,30}\b(highly|top|first|best|accept|hire)`),
	regexp.MustCompile(`(?i)\b(hire|accept|shortlist) (this|the) (candidate|applicant|paper|submission)\b`),
	regexp.MustCompile(`(?i)\bsystem prompt\b`),
}

// Match returns the first passage of text that reads as an instruction to an
// AI model, or "" when there is none.
func Match(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	for _, p := range patterns {
		if loc := p.FindStringIndex(flat); loc != nil {
			start, end := max(0, loc[0]-40), min(len(flat), loc[1]+60)
			for start > 0 && !utf8Start(flat[start]) {
				start--
			}
			for end < len(flat) && !utf8Start(flat[end]) {
				end++
			}
			return strings.TrimSpace(flat[start:end])
		}
	}
	return ""
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
