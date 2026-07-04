package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "gateway-auth-service/proto"
	pdpb "gateway-auth-service/proto/patientdata/v1"
)

type server struct {
	pb.UnimplementedAuthorizationServiceServer
	patientDataClient pdpb.PatientDataServiceClient
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s - Tempo: %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func (s *server) Authorize(ctx context.Context, req *pb.AuthorizeRequest) (*pb.AuthorizeResponse, error) {
	log.Printf("Validando autorização para usuário: %s, role: %s", req.Username, req.Role)

	if req.Role == "PESQUISADOR" {
		if req.ProjectId != "" {
			return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_ANONYMIZED, Message: "Acesso anonimizado concedido"}, nil
		}
		return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "Projeto não informado"}, nil
	}

	if req.TargetPatientId == "" {
		return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "ID do paciente não fornecido"}, nil
	}

	resp, err := s.patientDataClient.CheckAssignment(ctx, &pdpb.CheckAssignmentRequest{
		Username:  req.Username,
		PatientId: req.TargetPatientId,
		Role:      strings.ToLower(req.Role),
	})

	if err != nil {
		log.Printf("Erro ao consultar vínculo no banco: %v", err)
		return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "Erro interno ao validar vínculo"}, nil
	}

	if !resp.Allowed {
		return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "Profissional não possui vínculo com este paciente"}, nil
	}

	if req.Role == "MEDICO" {
		return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_FULL, Message: "Acesso total concedido"}, nil
	}
	
	if req.Role == "ESTAGIARIO" {
		return &pb.AuthorizeResponse{Decision: pb.Decision_ALLOW, AccessLevel: pb.AccessLevel_PARTIAL, Message: "Acesso parcial concedido"}, nil
	}

	return &pb.AuthorizeResponse{Decision: pb.Decision_DENY, AccessLevel: pb.AccessLevel_DENIED, Message: "Role não reconhecida"}, nil
}

func main() {
	grpcPort := getEnv("AUTH_GRPC_PORT", "50052")
	metricsPort := getEnv("AUTH_METRICS_PORT", "9091")
	patientDataTarget := getEnv("PATIENT_DATA_TARGET", "localhost:50051")

	pdConn, err := grpc.NewClient(patientDataTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao Patient Data Service: %v", err)
	}
	defer pdConn.Close()
	pdClient := pdpb.NewPatientDataServiceClient(pdConn)

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("Falha ao escutar: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	pb.RegisterAuthorizationServiceServer(s, &server{patientDataClient: pdClient})
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