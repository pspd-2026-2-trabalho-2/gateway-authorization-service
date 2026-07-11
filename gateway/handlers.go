package main

import (
	"context"
	pb "gateway-auth-service/proto"
	dtpb "gateway-auth-service/proto/datatransform/v1"
	pdpb "gateway-auth-service/proto/patientdata/v1"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func getUsernameFromRequest(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, _ := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if username, ok := claims["username"].(string); ok {
			return username
		}
	}
	return ""
}

func respondWithJSON(w http.ResponseWriter, fhirResponse proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	jsonBytes, _ := protojson.Marshal(fhirResponse)
	w.Write(jsonBytes)
}

func getPatientHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		patientID := r.PathValue("id")

		authResp, err := checkAuth(r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawPatient, err := pdClient.GetPatient(context.Background(), &pdpb.GetPatientRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar dados", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformPatient(context.Background(), &dtpb.TransformPatientRequest{
			Patient:     rawPatient,
			AccessLevel: mapAccessLevel(authResp.AccessLevel),
		})
		if err != nil {
			http.Error(w, "Erro na transformação", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := protojson.Marshal(fhirResponse)
		w.Write(jsonBytes)
	}
}

func getPatientSummaryHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		patientID := r.PathValue("id")

		authResp, err := checkAuth(r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawSummary, err := pdClient.GetClinicalSummary(context.Background(), &pdpb.GetClinicalSummaryRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar resumo", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformClinicalSummary(context.Background(), &dtpb.TransformClinicalSummaryRequest{
			Summary:     rawSummary,
			AccessLevel: mapAccessLevel(authResp.AccessLevel),
		})
		if err != nil {
			http.Error(w, "Erro na transformação", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := protojson.Marshal(fhirResponse)
		w.Write(jsonBytes)
	}
}

func getCohortStatisticsHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		condition := r.PathValue("condition")

		_, err := checkAuth(r, authClient, "", condition)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawStats, err := pdClient.GetCohortStatistics(context.Background(), &pdpb.GetCohortStatisticsRequest{ConditionCode: condition})
		if err != nil {
			http.Error(w, "Erro ao buscar estatísticas", http.StatusInternalServerError)
			return
		}

		aggResponse, err := dtClient.TransformCohortStatistics(context.Background(), &dtpb.TransformCohortStatisticsRequest{
			Stats: rawStats,
		})
		if err != nil {
			http.Error(w, "Erro na agregação", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := protojson.Marshal(aggResponse)
		w.Write(jsonBytes)
	}
}

func getDoctorPatientsHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := checkAuth(r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		rawList, err := pdClient.ListPatientsByDoctor(context.Background(), &pdpb.ListPatientsByDoctorRequest{DoctorUsername: username})
		if err != nil {
			http.Error(w, "Erro ao listar pacientes do médico", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformPatientList(context.Background(), &dtpb.TransformPatientListRequest{
			Patients:    rawList.Patients,
			AccessLevel: dtpb.AccessLevel_FULL,
		})
		if err != nil {
			http.Error(w, "Erro na formatação da lista", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}

func getInternPatientsHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := checkAuth(r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		rawList, err := pdClient.ListSupervisedPatients(context.Background(), &pdpb.ListSupervisedPatientsRequest{InternUsername: username})
		if err != nil {
			http.Error(w, "Erro ao listar pacientes supervisionados", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformPatientList(context.Background(), &dtpb.TransformPatientListRequest{
			Patients:    rawList.Patients,
			AccessLevel: dtpb.AccessLevel_PARTIAL,
		})
		if err != nil {
			http.Error(w, "Erro na formatação da lista", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}

func getResearcherProjectsHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := checkAuth(r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		rawList, err := pdClient.ListProjectsByResearcher(context.Background(), &pdpb.ListProjectsByResearcherRequest{ResearcherUsername: username})
		if err != nil {
			http.Error(w, "Erro ao listar projetos de pesquisa", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformProjects(context.Background(), &dtpb.TransformProjectsRequest{
			Projects: rawList.Projects,
		})
		if err != nil {
			http.Error(w, "Erro na formatação de projetos", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}

func getPatientHistoryHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		patientID := r.PathValue("id")
		authResp, err := checkAuth(r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawPatient, err := pdClient.GetPatient(context.Background(), &pdpb.GetPatientRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar paciente", http.StatusInternalServerError)
			return
		}

		rawHistory, err := pdClient.GetClinicalHistory(context.Background(), &pdpb.GetClinicalHistoryRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar histórico", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformClinicalHistory(context.Background(), &dtpb.TransformClinicalHistoryRequest{
			Patient:     rawPatient,
			Events:      rawHistory.Events,
			AccessLevel: mapAccessLevel(authResp.AccessLevel),
		})
		if err != nil {
			http.Error(w, "Erro na transformação de histórico", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}

func getCohortExamsHandler(authClient pb.AuthorizationServiceClient, pdClient pdpb.PatientDataServiceClient, dtClient dtpb.DataTransformServiceClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		condition := r.PathValue("condition")
		authResp, err := checkAuth(r, authClient, "", condition)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawPatients, err := pdClient.ListCohortPatients(context.Background(), &pdpb.ListCohortPatientsRequest{ConditionCode: condition})
		if err != nil {
			http.Error(w, "Erro ao buscar pacientes da coorte", http.StatusInternalServerError)
			return
		}

		var patientExamsList []*dtpb.PatientExams
		for _, patient := range rawPatients.Patients {
			eventsResp, err := pdClient.ListClinicalEvents(context.Background(), &pdpb.ListClinicalEventsRequest{
				PatientId: patient.GetPatientId(),
				EventType: "Observation",
			})
			if err == nil {
				patientExamsList = append(patientExamsList, &dtpb.PatientExams{
					Patient: patient,
					Exams:   eventsResp.Events,
				})
			}
		}

		fhirResponse, err := dtClient.TransformCohortExams(context.Background(), &dtpb.TransformCohortExamsRequest{
			Patients:    patientExamsList,
			AccessLevel: mapAccessLevel(authResp.AccessLevel),
		})
		if err != nil {
			http.Error(w, "Erro na transformação dos exames da coorte", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}