package response

import (
	"encoding/json"
	"net/http"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

func JSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func Success(w http.ResponseWriter, status int, data interface{}) {
	JSON(w, status, Response{Success: true, Data: data})
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, Response{Success: false, Message: message})
}
