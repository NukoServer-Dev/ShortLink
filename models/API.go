package models

type CreateShortLink struct {
	Url string
}

type ExtensionLink struct {
	Code string
}

type CreateResponse struct {
	Code   string
	Status string
}

type ErrorResponse struct {
	Error  string
	Status string
}

type OKResponse struct {
	Message string
	Status  string
}
