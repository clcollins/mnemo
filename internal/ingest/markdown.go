package ingest

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
)

type MarkdownChunker struct {
	targetTokens int
	overlap      int
}

func NewMarkdownChunker(targetTokens, overlap int) *MarkdownChunker {
	return &MarkdownChunker{
		targetTokens: targetTokens,
		overlap:      overlap,
	}
}

func (m *MarkdownChunker) Chunk(doc Document) ([]RawChunk, error) {
	content := stripFrontmatter(doc.Content)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	sections := splitByHeadings(content)
	var chunks []RawChunk

	for _, sec := range sections {
		if strings.TrimSpace(sec.content) == "" {
			continue
		}

		tokenEst := estimateTokens(sec.content)
		if tokenEst <= m.targetTokens || m.targetTokens == 0 {
			chunks = append(chunks, RawChunk{
				Content:     strings.TrimSpace(sec.content),
				ContentHash: hashContent(sec.content),
				Source:      doc.Path,
				HeadingPath: sec.headingPath,
			})
		} else {
			subChunks := splitLargeSection(sec, m.targetTokens)
			for _, sc := range subChunks {
				chunks = append(chunks, RawChunk{
					Content:     strings.TrimSpace(sc.content),
					ContentHash: hashContent(sc.content),
					Source:      doc.Path,
					HeadingPath: sc.headingPath,
				})
			}
		}
	}

	return chunks, nil
}

type section struct {
	headingPath string
	content     string
	level       int
}

func splitByHeadings(content string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	var headingStack []string
	var currentContent strings.Builder
	currentLevel := 0
	inCodeFence := false

	flushSection := func() {
		text := currentContent.String()
		if strings.TrimSpace(text) != "" {
			sections = append(sections, section{
				headingPath: strings.Join(headingStack, " > "),
				content:     text,
				level:       currentLevel,
			})
		}
		currentContent.Reset()
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCodeFence = !inCodeFence
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
			continue
		}

		if inCodeFence {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
			continue
		}

		level := headingLevel(line)
		if level > 0 {
			flushSection()
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			updateHeadingStack(&headingStack, level, title)
			currentLevel = level
			continue
		}

		currentContent.WriteString(line)
		currentContent.WriteString("\n")
	}

	flushSection()
	return sections
}

func headingLevel(line string) int {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "#") {
		return 0
	}
	level := 0
	for _, ch := range trimmed {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level > 0 && level <= 6 && len(trimmed) > level && trimmed[level] == ' ' {
		return level
	}
	return 0
}

func updateHeadingStack(stack *[]string, level int, title string) {
	for len(*stack) >= level {
		*stack = (*stack)[:len(*stack)-1]
	}
	*stack = append(*stack, title)
}

func splitLargeSection(sec section, targetTokens int) []section {
	paragraphs := splitParagraphs(sec.content)
	var sections []section
	var currentParagraphs []string
	currentTokens := 0

	for _, para := range paragraphs {
		paraTokens := estimateTokens(para)
		if currentTokens+paraTokens > targetTokens && len(currentParagraphs) > 0 {
			sections = append(sections, section{
				headingPath: sec.headingPath,
				content:     strings.Join(currentParagraphs, "\n\n"),
				level:       sec.level,
			})
			currentParagraphs = nil
			currentTokens = 0
		}
		currentParagraphs = append(currentParagraphs, para)
		currentTokens += paraTokens
	}

	if len(currentParagraphs) > 0 {
		sections = append(sections, section{
			headingPath: sec.headingPath,
			content:     strings.Join(currentParagraphs, "\n\n"),
			level:       sec.level,
		})
	}

	return sections
}

func splitParagraphs(content string) []string {
	raw := strings.Split(content, "\n\n")
	var paragraphs []string
	var codeBlock strings.Builder
	inCode := false

	for _, p := range raw {
		fenceCount := strings.Count(p, "```")
		if inCode {
			codeBlock.WriteString("\n\n")
			codeBlock.WriteString(p)
			if fenceCount%2 != 0 {
				inCode = false
				paragraphs = append(paragraphs, codeBlock.String())
				codeBlock.Reset()
			}
			continue
		}
		if fenceCount%2 != 0 {
			inCode = true
			codeBlock.WriteString(p)
			continue
		}
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	if codeBlock.Len() > 0 {
		paragraphs = append(paragraphs, codeBlock.String())
	}

	return paragraphs
}

func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := content[3:]
	idx := strings.Index(rest, "---")
	if idx < 0 {
		return content
	}
	return strings.TrimSpace(rest[idx+3:])
}

func estimateTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / 4.0))
}

func hashContent(content string) string {
	normalized := strings.TrimSpace(content)
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)
}
