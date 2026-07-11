package main

import (
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "gateway-auth-service/proto"
	dtpb "gateway-auth-service/proto/datatransform/v1"
	pdpb "gateway-auth-service/proto/patientdata/v1"
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return fallback
}

func main() {
	port := getEnv("GATEWAY_PORT", "8080")
	jwtSecret = []byte(getEnv("JWT_SECRET", "secret-key"))

	authTarget := getEnv("AUTH_SERVICE_TARGET", "localhost:50052")
	patientDataTarget := getEnv("PATIENT_DATA_TARGET", "localhost:50051")
	dataTransformTarget := getEnv("DATA_TRANSFORM_TARGET", "localhost:50053")

	authConn, err := grpc.NewClient(authTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao AuthService: %v", err)
	}
	defer authConn.Close()
	authClient := pb.NewAuthorizationServiceClient(authConn)

	pdConn, err := grpc.NewClient(patientDataTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao PatientDataService: %v", err)
	}
	defer pdConn.Close()
	pdClient := pdpb.NewPatientDataServiceClient(pdConn)

	dtConn, err := grpc.NewClient(dataTransformTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao DataTransformService: %v", err)
	}
	defer dtConn.Close()
	dtClient := dtpb.NewDataTransformServiceClient(dtConn)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/patients/{id}", getPatientHandler(authClient, pdClient, dtClient))
	mux.HandleFunc("GET /api/patients/{id}/summary", getPatientSummaryHandler(authClient, pdClient, dtClient))
	mux.HandleFunc("GET /api/patients/{id}/history", getPatientHistoryHandler(authClient, pdClient, dtClient))

	mux.HandleFunc("GET /api/cohorts/{condition}/statistics", getCohortStatisticsHandler(authClient, pdClient, dtClient))
	mux.HandleFunc("GET /api/cohorts/{condition}/exams", getCohortExamsHandler(authClient, pdClient, dtClient))

	mux.HandleFunc("GET /api/me/patients", getDoctorPatientsHandler(authClient, pdClient, dtClient))
	mux.HandleFunc("GET /api/me/supervised-patients", getInternPatientsHandler(authClient, pdClient, dtClient))
	mux.HandleFunc("GET /api/me/projects", getResearcherProjectsHandler(authClient, pdClient, dtClient))
	
	mux.Handle("/metrics", promhttp.Handler())

	loggedMux := loggingMiddleware(mux)

	log.Println("API Gateway rodando na porta 8080...")
	log.Fatal(http.ListenAndServe(":" + port, loggedMux))
}