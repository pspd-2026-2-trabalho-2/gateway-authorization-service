package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "gateway-auth-service/proto"
)

var limiter = rate.NewLimiter(10, 20)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return fallback
}

func validateJWT(tokenString string, secret []byte) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", token.Header["alg"])
		}
		return secret, nil
	})

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, err
}

func patientHandler(authClient pb.AuthorizationServiceClient, jwtSecret []byte) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !limiter.Allow() {
            http.Error(w, "Muitas solicitações", http.StatusTooManyRequests)
            return
        }

        authHeader := r.Header.Get("Authorization")
        if authHeader == "" {
            http.Error(w, "Token não fornecido", http.StatusUnauthorized)
            return
        }

        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        claims, err := validateJWT(tokenString, jwtSecret)
        if err != nil {
            http.Error(w, "Token JWT inválido", http.StatusUnauthorized)
            return
        }

        username, ok := claims["username"].(string)
        if !ok {
            http.Error(w, "Erro ao ler 'username' do token", http.StatusUnauthorized)
            return
        }

        role, ok := claims["role"].(string)
        if !ok {
            http.Error(w, "Erro ao ler 'role' do token", http.StatusUnauthorized)
            return
        }

        patientID := r.URL.Query().Get("patient_id")

        authResp, err := authClient.Authorize(context.Background(), &pb.AuthorizeRequest{
            Username:        username,
            Role:            role,
            TargetPatientId: patientID,
        })

        if err != nil {
            log.Printf("Erro de comunicação com o Auth Service: %v", err)
            http.Error(w, "Erro interno ao validar autorização", http.StatusInternalServerError)
            return
        }

        if authResp.Decision == pb.Decision_DENY {
            http.Error(w, "Acesso Negado: "+authResp.Message, http.StatusForbidden)
            return
        }

        // TODO: Encaminhar para PatientData/DataTransform via gRPC baseado no AccessLevel retornado

        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, "Acesso permitido! Nível: %v\n", authResp.AccessLevel)
    }
}

func logginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		
		log.Printf("[%s] %s %s - Tempo: %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func main() {
	port := getEnv("GATEWAY_PORT", "8080")
	jwtSecret := []byte(getEnv("JWT_SECRET", "secret-key"))
	authTarget := getEnv("AUTH_SERVICE_TARGET", "localhost:50051")

	conn, err := grpc.NewClient(authTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao AuthService: %v", err)
	}

	defer conn.Close()
	authClient := pb.NewAuthorizationServiceClient(conn)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/patients", patientHandler(authClient, jwtSecret))
	mux.Handle("/metrics", promhttp.Handler())

	loggedMux := logginMiddleware(mux)

	log.Println("API Gateway rodando na porta 8080...")
	log.Fatal(http.ListenAndServe(":" + port, loggedMux))
}