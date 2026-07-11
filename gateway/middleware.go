package main

import (
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

var limiter = rate.NewLimiter(10, 20)
var corsAllowedOrigin string

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == corsAllowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", corsAllowedOrigin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, "Muitas solicitações", http.StatusTooManyRequests)
			return
		}

		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s - Tempo: %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}
