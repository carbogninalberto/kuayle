package dto

// WikiTreeNodeResponse is one page's place in its wiki's tree.
type WikiTreeNodeResponse struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Position int     `json:"position"`
	Kind     string  `json:"kind"`
}

// MoveWikiPageRequest puts a page under ParentID (null for the top level)
// at Index among its new siblings, counted without the page.
type MoveWikiPageRequest struct {
	ParentID *string `json:"parent_id"`
	Index    int     `json:"index"`
}

// CreateWikiTreePageRequest creates a page ("page") or folder ("folder").
type CreateWikiTreePageRequest struct {
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	Kind     string  `json:"kind"`
	ParentID *string `json:"parent_id"`
}
