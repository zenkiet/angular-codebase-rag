package rag

type SearchRequest struct {
	Query string
	Limit int
	Kind  string
	Path  string
}

type SearchResponse struct {
	Summary string   `json:"summary"`
	Project string   `json:"project"`
	Query   string   `json:"query"`
	Results []Result `json:"results"`
}

type Result struct {
	Rank         int     `json:"rank"`
	ID           string  `json:"id"`
	Score        float32 `json:"score"`
	RelativePath string  `json:"relative_path"`
	StartLine    int     `json:"start_line"`
	EndLine      int     `json:"end_line"`
	Kind         string  `json:"kind"`
	Symbol       string  `json:"symbol"`
	ParentSymbol string  `json:"parent_symbol,omitempty"`
	Snippet      string  `json:"snippet"`
}
