package main

import (
	"context"
	pb "gateway-auth-service/proto"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedAuthorizationServiceServer
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func (s *server) Authorize(ctx context.Context, req *pb.AuthorizeRequest) (*pb.AuthorizeResponse, error) {
	log.Printf("Validando autorização para usuário: %s, role: %s", req.Username, req.Role)

	switch req.Role {
	case "MEDICO":
		if req.TargetPatientId != "" {
			return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_FULL, Message: "Acesso total concedido"}, nil
		}

	case "ESTAGIARIO":
		if req.TargetPatientId != "" {
			return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_PARTIAL, Message: "Acesso parcial concedido"}, nil
		}

	case "PESQUISADOR":
		if req.TargetPatientId != "" {
			return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_ANONYMIZED, Message: "Acesso anônimo concedido"}, nil
		}
	}

	return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "Acesso negado"}, nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s - Tempo: %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func main() {
	grpcPort := getEnv("AUTH_GRPC_PORT", "50051")
	metricsPort := getEnv("AUTH_METRICS_PORT", "9090")

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("Falha ao escutar: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	pb.RegisterAuthorizationServiceServer(s, &server{})
	grpc_prometheus.Register(s)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		
		loggedMux := loggingMiddleware(mux)
		
		log.Printf("Métricas do Auth Service rodando na porta %s...", metricsPort)
		log.Fatal(http.ListenAndServe(":"+metricsPort, loggedMux))
	}()

	log.Printf("Authorization Service rodando na porta %s...", grpcPort)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Falha ao servir gRPC: %v", err)
	}
}