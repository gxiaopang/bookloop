package dto

type Response struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id"`
}

func SuccessResponse(data any, requestID string) Response {
	return Response{
		Code:      "OK",
		Message:   "success",
		Data:      data,
		RequestID: requestID,
	}
}

func ErrorResponse(code, message, requestID string) Response {
	return Response{
		Code:      code,
		Message:   message,
		RequestID: requestID,
	}
}
