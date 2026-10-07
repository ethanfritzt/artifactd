package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Render converts common Markdown blocks and inline text to a minimal DOCX file.
func Render(source []byte) ([]byte, error) {
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	paragraphs := make([]string, 0, 16)
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		collect(node, source, &paragraphs)
	}
	var document strings.Builder
	document.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		document.WriteString("<w:p><w:r><w:t xml:space=\"preserve\">")
		if err := xml.EscapeText(&document, []byte(paragraph)); err != nil {
			return nil, fmt.Errorf("encoding DOCX text: %w", err)
		}
		document.WriteString("</w:t></w:r></w:p>")
	}
	document.WriteString(`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`)

	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	files := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", document.String()},
	}
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			return nil, fmt.Errorf("creating DOCX part: %w", err)
		}
		if _, err := io.WriteString(writer, file.content); err != nil {
			return nil, fmt.Errorf("writing DOCX part: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finalizing DOCX: %w", err)
	}
	return output.Bytes(), nil
}

func collect(node ast.Node, source []byte, output *[]string) {
	switch node.Kind() {
	case ast.KindHeading, ast.KindParagraph:
		*output = append(*output, inlineText(node, source))
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		var code strings.Builder
		if lines := node.Lines(); lines != nil {
			for index := 0; index < lines.Len(); index++ {
				segment := lines.At(index)
				code.Write(segment.Value(source))
			}
		}
		*output = append(*output, code.String())
	case ast.KindList:
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			if item.Kind() != ast.KindListItem {
				continue
			}
			text := inlineText(item, source)
			prefix := "• "
			if list, ok := node.(*ast.List); ok && list.IsOrdered() {
				prefix = "1. "
			}
			*output = append(*output, prefix+text)
		}
	case ast.KindThematicBreak:
		*output = append(*output, "────────────────")
	case ast.KindBlockquote:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			collect(child, source, output)
		}
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			collect(child, source, output)
		}
	}
}

func inlineText(node ast.Node, source []byte) string {
	var text strings.Builder
	var visit func(ast.Node)
	visit = func(current ast.Node) {
		if value, ok := current.(*ast.Text); ok {
			text.Write(value.Segment.Value(source))
			if value.SoftLineBreak() || value.HardLineBreak() {
				text.WriteByte('\n')
			}
			return
		}
		if value, ok := current.(*ast.String); ok {
			text.Write(value.Value)
			return
		}
		for child := current.FirstChild(); child != nil; child = child.NextSibling() {
			visit(child)
		}
	}
	visit(node)
	return text.String()
}
