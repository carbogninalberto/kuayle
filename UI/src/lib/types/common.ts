export interface PaginatedResponse<T> {
	data: T[];
	total_count: number;
	page: number;
	per_page: number;
	has_more: boolean;
}

export interface ApiError {
	error: {
		code: string;
		message: string;
		details?: { field: string; message: string }[];
	};
}
