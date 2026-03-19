package dto

type PaginationParams struct {
	Page    int `query:"page" validate:"min=1"`
	PerPage int `query:"per_page" validate:"min=1,max=100"`
}

func (p *PaginationParams) Defaults() {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PerPage == 0 {
		p.PerPage = 50
	}
}

func (p *PaginationParams) Offset() int {
	return (p.Page - 1) * p.PerPage
}

type ListResponse[T any] struct {
	Data       []T  `json:"data"`
	TotalCount int  `json:"total_count"`
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	HasMore    bool `json:"has_more"`
}

type ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type ErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}
