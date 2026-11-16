package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"shortlink/consts"
	"shortlink/lib"
	"shortlink/models"

	"gorm.io/gorm"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/index.html")
	})

	mux.HandleFunc("GET /s/{code}", func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")

		var checkShortjson models.Links
		check := lib.DB.Where("code = ?", code).First(&checkShortjson)
		if check.Error != nil {
			if errors.Is(check.Error, gorm.ErrRecordNotFound) {
				http.ServeFile(w, r, "public/notfound.html")
			}
		}
		http.Redirect(w, r, checkShortjson.Url, http.StatusFound)
	})

	mux.HandleFunc("POST /api/create", func(w http.ResponseWriter, r *http.Request) {
		var payload models.CreateShortLink
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			http.Error(w, "不正なJSON形式", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if strings.HasPrefix(payload.Url, "https://") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(models.ErrorResponse{
				Status: "error",
				Error:  "不正なURLです。",
			})
			return
		}

		if consts.IsInclude(payload.Url) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(models.ErrorResponse{
				Status: "error",
				Error:  "禁止されたURLです。",
			})
			return
		}

		code, genErr := lib.GenerateRandomString(10)
		if genErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(models.ErrorResponse{
				Status: "error",
				Error:  "サーバー側でエラーが発生しました。",
			})
			return
		}

		var checkShortjson models.Links
		check := lib.DB.Where("code = ?", code).First(&checkShortjson)
		if check.Error != nil {
			if errors.Is(check.Error, gorm.ErrRecordNotFound) {
				shortJson := models.Links{
					Code:      code,
					Url:       payload.Url,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
					CreatorIP: r.RemoteAddr,
				}
				lib.DB.Create(&shortJson)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(models.CreateResponse{
					Status: "ok",
					Code:   code,
				})
				return
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(models.ErrorResponse{
					Status: "error",
					Error:  "サーバー側でエラーが発生しました。",
				})
				return
			}
		}

		if time.Now().After(checkShortjson.UpdatedAt.AddDate(0, 0, 7)) {
			lib.DB.Delete(&models.Links{}, checkShortjson.ID)

			shortJson := models.Links{
				Code:      code,
				Url:       payload.Url,
				CreatedAt: time.Now(),
				CreatorIP: r.RemoteAddr,
			}
			lib.DB.Create(&shortJson)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(models.CreateResponse{
				Status: "ok",
				Code:   code,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.ErrorResponse{
			Status: "error",
			Error:  "現在混み合っています。数分待ってからお試してください。",
		})
		return
	})

	fmt.Println("Servering on :3000...")
	serverErr := http.ListenAndServe("0.0.0.0:3000", mux)
	if serverErr != nil {
		panic(serverErr)
	}
}
