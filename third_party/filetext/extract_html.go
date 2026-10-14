package filetext

import (
	"strings"

	"golang.org/x/net/html"
)

// ExtractTextFromHTML removes markup while retaining visible text and useful
// block boundaries. Script, style, template, and noscript content is omitted.
func ExtractTextFromHTML(filePath string) (string, error) {
	content, err := ReadTextFile(filePath)
	if err != nil {
		return "", err
	}
	return stripHTMLTags(content), nil
}

func stripHTMLTags(input string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(input))
	var output strings.Builder
	skipDepth := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch tokenType {
		case html.TextToken:
			if skipDepth == 0 {
				appendHTMLText(&output, token.Data)
			}
		case html.StartTagToken:
			tag := strings.ToLower(token.Data)
			if tag == "script" || tag == "style" || tag == "template" || tag == "noscript" {
				skipDepth++
			} else if skipDepth == 0 && tag == "br" {
				appendHTMLSeparator(&output, '\n')
			}
		case html.EndTagToken:
			tag := strings.ToLower(token.Data)
			if skipDepth > 0 && (tag == "script" || tag == "style" || tag == "template" || tag == "noscript") {
				skipDepth--
				continue
			}
			if skipDepth != 0 {
				continue
			}
			switch tag {
			case "p", "div", "section", "article", "header", "footer", "aside", "nav",
				"li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre":
				appendHTMLSeparator(&output, '\n')
			case "td", "th":
				appendHTMLSeparator(&output, '\t')
			}
		}
	}
	return strings.TrimSpace(output.String())
}

func appendHTMLText(output *strings.Builder, value string) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return
	}
	if output.Len() > 0 {
		last := output.String()[output.Len()-1]
		if last != '\n' && last != '\t' && last != ' ' {
			output.WriteByte(' ')
		}
	}
	output.WriteString(value)
}

func appendHTMLSeparator(output *strings.Builder, separator byte) {
	if output.Len() == 0 {
		return
	}
	value := output.String()
	last := value[len(value)-1]
	if last == '\n' || last == separator {
		return
	}
	output.WriteByte(separator)
}
