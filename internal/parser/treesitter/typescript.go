package treesitter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var (
	regexDecorator = regexp.MustCompile(`@([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

type TypeScriptParser struct {
	cfg      config.Config
	language *sitter.Language
}

type declaration struct {
	node              sitter.Node
	start, end        uint
	kind, sym, parent string
}

func NewTypeScriptParser(cfg config.Config) *TypeScriptParser {
	return &TypeScriptParser{cfg: cfg, language: sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())}
}

func (p *TypeScriptParser) ParseFile(ctx context.Context, filePath string) ([]domain.CodeChunk, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	relPath, _ := filepath.Rel(p.cfg.Project.Root, filePath)
	if relPath == "" {
		relPath = filepath.Base(filePath)
	}
	relPath = filepath.ToSlash(relPath)

	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == ".ts" || ext == ".js" {
		return p.parseTypeScript(ctx, filePath, relPath, content)
	}
	return p.parseAsset(filePath, relPath, content), nil
}

func (p *TypeScriptParser) parseTypeScript(ctx context.Context, filePath, relPath string, src []byte) ([]domain.CodeChunk, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(p.language)

	tree := parser.ParseCtx(ctx, src, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse cancelled")
	}
	defer tree.Close()

	decls := p.extractDecls(*tree.RootNode(), src)

	if len(decls) == 0 {
		chunks := p.makeLineChunks(filePath, relPath, "file", toPascal(filepath.Base(relPath)), "", string(src), 1)
		return chunks, nil
	}

	sort.Slice(decls, func(i, j int) bool { return decls[i].start < decls[j].start })

	var chunks []domain.CodeChunk
	for _, d := range decls {
		chunks = append(chunks, p.chunkDeclaration(filePath, relPath, d, src)...)
	}

	return chunks, nil
}

func (p *TypeScriptParser) parseAsset(filePath, relPath string, src []byte) []domain.CodeChunk {
	kind, sym := "file", toPascal(strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)))

	if strings.HasSuffix(relPath, ".component.html") || strings.Contains(relPath, ".component.") {
		kind = func() string {
			if strings.HasSuffix(relPath, ".html") {
				return "template"
			}
			return "style"
		}()
		sym = toPascal(strings.Split(filepath.Base(relPath), ".")[0]) + "Component"
	}
	return p.makeLineChunks(filePath, relPath, kind, sym, "", string(src), 1)
}

func (p *TypeScriptParser) extractDecls(node sitter.Node, src []byte) []declaration {
	var decls []declaration
	cursor := node.Walk()
	defer cursor.Close()

	for _, child := range node.NamedChildren(cursor) {
		raw := child
		child, start, end, ok := unwrapDeclaration(child)
		if !ok {
			continue
		}

		k := child.Kind()
		switch k {
		case "class_declaration":
			if decoratorStart := expandDecoratorStart(raw); decoratorStart < start {
				start = decoratorStart
			}
			if decoratorStart := expandDecoratorStart(child); decoratorStart < start {
				start = decoratorStart
			}
			sym := nodeName(child, src, "anonymous_class")
			kind := classKind(string(src[start:end]))
			decls = append(decls, declaration{child, start, end, kind, sym, ""})
		case "interface_declaration", "type_alias_declaration", "enum_declaration", "function_declaration":
			kind := strings.Split(k, "_")[0]
			decls = append(decls, declaration{child, start, end, kind, nodeName(child, src, kind), ""})
		case "lexical_declaration", "variable_declaration":
			for _, v := range child.NamedChildren(child.Walk()) {
				if v.Kind() == "variable_declarator" {
					sym := nodeName(v, src, "")
					if strings.Contains(strings.ToLower(sym), "route") {
						decls = append(decls, declaration{v, start, end, "route", sym, ""})
					}
				}
			}
		}
	}
	return decls
}

func unwrapDeclaration(node sitter.Node) (sitter.Node, uint, uint, bool) {
	start, end := node.StartByte(), node.EndByte()
	if isDeclarationKind(node.Kind()) {
		return node, start, end, true
	}
	if node.Kind() != "export_statement" {
		return node, start, end, false
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if isDeclarationKind(child.Kind()) {
			return child, start, end, true
		}
	}
	return node, start, end, false
}

func isDeclarationKind(kind string) bool {
	switch kind {
	case "class_declaration",
		"interface_declaration",
		"type_alias_declaration",
		"enum_declaration",
		"function_declaration",
		"lexical_declaration",
		"variable_declaration":
		return true
	default:
		return false
	}
}

func (p *TypeScriptParser) chunkDeclaration(filePath, relPath string, d declaration, src []byte) []domain.CodeChunk {
	text := strings.TrimSpace(string(src[d.start:d.end]))
	if len(text) <= p.cfg.Chunking.MaxChars {
		return p.buildChunks(filePath, relPath, d.kind, d.sym, d.parent, d.start, d.end, src)
	}

	if body := d.node.ChildByFieldName("body"); body != nil {
		var chunks []domain.CodeChunk
		chunks = append(chunks, p.buildChunks(filePath, relPath, d.kind, d.sym, d.parent, d.start, body.StartByte(), src)...)

		for _, child := range body.NamedChildren(body.Walk()) {
			start, end := child.StartByte(), child.EndByte()
			if end <= start {
				continue
			}

			kind := "property"
			if strings.Contains(child.Kind(), "method") {
				kind = "method"
			} else if strings.Contains(child.Kind(), "constructor") {
				kind = "constructor"
			}
			sym := nodeName(child, src, kind)

			if len(strings.TrimSpace(string(src[start:end]))) <= p.cfg.Chunking.MaxChars {
				chunks = append(chunks, p.buildChunks(filePath, relPath, kind, sym, d.sym, start, end, src)...)
			} else {
				chunks = append(chunks, p.makeLineChunks(filePath, relPath, kind, sym, d.sym, string(src[start:end]), lineNum(src, start))...)
			}
		}
		if len(chunks) > 0 {
			return chunks
		}
	}
	return p.makeLineChunks(filePath, relPath, d.kind, d.sym, d.parent, text, lineNum(src, d.start))
}

func (p *TypeScriptParser) buildChunks(filePath, relPath, kind, sym, parent string, start, end uint, src []byte) []domain.CodeChunk {
	text := strings.TrimSpace(string(src[start:end]))
	if max := p.cfg.Chunking.MaxChars; max > 0 && len(text) > max {
		return p.makeLineChunks(filePath, relPath, kind, sym, parent, text, lineNum(src, start))
	}
	if text == "" {
		return nil
	}
	return p.finalize([]domain.CodeChunk{newChunk(filePath, relPath, kind, sym, parent, lineNum(src, start), lineNum(src, end), text, p.cfg.Project.Name)})
}

func (p *TypeScriptParser) makeLineChunks(filePath, relPath, kind, sym, parent, text string, startLine int) []domain.CodeChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	max := p.cfg.Chunking.MaxChars
	if max <= 0 || len(text) <= max {
		return p.finalize([]domain.CodeChunk{newChunk(filePath, relPath, kind, sym, parent, startLine, startLine+strings.Count(text, "\n"), text, p.cfg.Project.Name)})
	}

	var chunks []domain.CodeChunk
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		if len(lines[i]) > max {
			for _, part := range splitLongLine(lines[i], max) {
				content := strings.TrimSpace(part)
				if content != "" {
					chunks = append(chunks, newChunk(filePath, relPath, kind, sym, parent, startLine+i, startLine+i, content, p.cfg.Project.Name))
				}
			}
			i++
			continue
		}

		start, length := i, 0
		for i < len(lines) {
			lineLen := len(lines[i])
			if lineLen > max {
				break
			}
			if length > 0 {
				lineLen++
			}
			if length+lineLen > max {
				break
			}
			length += lineLen
			i++
		}
		if i == start {
			i++
		} // move to next line if the line is too long

		content := strings.TrimSpace(strings.Join(lines[start:i], "\n"))
		if content != "" {
			chunks = append(chunks, newChunk(filePath, relPath, kind, sym, parent, startLine+start, startLine+i-1, content, p.cfg.Project.Name))
		}
		if i < len(lines) {
			i = maxInt(start+1, i-p.cfg.Chunking.OverlapLines)
		}
	}
	return p.finalize(chunks)
}

func splitLongLine(line string, max int) []string {
	if max <= 0 || len(line) <= max {
		return []string{line}
	}

	parts := make([]string, 0, len(line)/max+1)
	for len(line) > max {
		cut := max
		for cut > 0 && cut < len(line) && !utf8.RuneStart(line[cut]) {
			cut--
		}
		if cut == 0 {
			cut = max
		}
		parts = append(parts, line[:cut])
		line = line[cut:]
	}
	if line != "" {
		parts = append(parts, line)
	}
	return parts
}

func newChunk(file, rel, kind, sym, parent string, sl, el int, txt, proj string) domain.CodeChunk {
	contentHash := domain.ContentHash(txt)
	return domain.CodeChunk{
		HashID:      domain.GenerateCodeChunkID(proj, rel, kind, sym, parent, strconv.Itoa(sl), strconv.Itoa(el), contentHash),
		ProjectName: proj, FilePath: file, RelativePath: rel, Language: domain.LanguageFromPath(file),
		ChunkKind: kind, SymbolName: sym, ParentSymbol: parent, StartLine: sl, EndLine: el,
		Content: txt,
	}
}

func (p *TypeScriptParser) finalize(chunks []domain.CodeChunk) []domain.CodeChunk {
	for i := range chunks {
		chunks[i].EmbedText = fmt.Sprintf("Project: %s\nFile: %s:%d-%d\nLanguage: %s\nKind: %s\nSymbol: %s\nCode:\n%s",
			chunks[i].ProjectName, chunks[i].RelativePath, chunks[i].StartLine, chunks[i].EndLine, chunks[i].Language, chunks[i].ChunkKind, chunks[i].SymbolName, chunks[i].Content)
	}
	return chunks
}

func lineNum(src []byte, pos uint) int { return bytes.Count(src[:pos], []byte{'\n'}) + 1 }
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func nodeName(n sitter.Node, src []byte, fallback string) string {
	if name := n.ChildByFieldName("name"); name != nil {
		return strings.TrimSpace(name.Utf8Text(src))
	}
	for _, child := range n.NamedChildren(n.Walk()) {
		if strings.Contains(child.Kind(), "identifier") {
			return strings.TrimSpace(child.Utf8Text(src))
		}
	}
	return fallback
}

func classKind(text string) string {
	for _, match := range regexDecorator.FindAllStringSubmatch(text, -1) {
		switch strings.ToLower(match[1]) {
		case "component", "injectable", "ngmodule", "directive", "pipe":
			return angularKind(match[1])
		}
	}
	return "class"
}

func angularKind(decorator string) string {
	switch strings.ToLower(decorator) {
	case "injectable":
		return "service"
	case "ngmodule":
		return "module"
	default:
		return strings.ToLower(decorator)
	}
}

func expandDecoratorStart(n sitter.Node) uint {
	start := n.StartByte()
	for prev := n.PrevNamedSibling(); prev != nil && prev.Kind() == "decorator"; prev = prev.PrevNamedSibling() {
		start = prev.StartByte()
	}
	return start
}

func toPascal(val string) string {
	parts := strings.FieldsFunc(val, func(r rune) bool { return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
