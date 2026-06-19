package memory

import "strings"

type classification struct {
	keywords   []string
	category   string
	importance float64
}

var classifications = []classification{
	{keywords: []string{"decided", "decision", "chose", "chosen"}, category: "decision", importance: 0.8},
	{keywords: []string{"learned", "lesson", "realized", "discovered"}, category: "lesson", importance: 0.75},
	{keywords: []string{"prefer", "preference", "rather", "like to"}, category: "preference", importance: 0.7},
	{keywords: []string{"always", "never", "pattern", "convention", "rule"}, category: "pattern", importance: 0.7},
}

func Classify(text string) (category string, importance float64) {
	lower := strings.ToLower(text)
	for _, c := range classifications {
		for _, kw := range c.keywords {
			if strings.Contains(lower, kw) {
				return c.category, c.importance
			}
		}
	}
	return "fact", 0.5
}
