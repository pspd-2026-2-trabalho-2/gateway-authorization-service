package main

import (
	"context"
	"fmt"
	pb "gateway-auth-service/proto"
	dtpb "gateway-auth-service/proto/datatransform/v1"
	pdpb "gateway-auth-service/proto/patientdata/v1"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Paginação de listas de pacientes: precisa bater com os limites do
// patient-data-service (internal/service.limitOffset) para que hasMore seja
// calculado corretamente a partir do tamanho de página efetivamente pedido.
const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// downstreamTimeout é o deadline default de toda chamada gRPC downstream
// (auth/patient-data/data-transform). Definido em main() a partir de
// DOWNSTREAM_TIMEOUT.
var downstreamTimeout = 15 * time.Second

// downstreamCtx deriva do contexto da requisição HTTP: se o cliente desiste
// (conexão fechada, timeout do lado dele), o cancelamento se propaga para as
// chamadas gRPC em andamento em vez de deixá-las órfãs disputando o pool do
// patient-data-service. O timeout garante um teto mesmo que o cliente nunca
// desista e o serviço downstream não imponha deadline nenhum.
func downstreamCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), downstreamTimeout)
}

func getUsernameFromRequest(r *http.Request) string {
	claims, err := parseClaims(r)
	if err != nil {
		return ""
	}
	username, _ := usernameAndRole(claims)
	return username
}

// parsePagination lê ?page=&pageSize= da query string, com defaults e limites
// seguros (mesmos do patient-data-service).
func parsePagination(r *http.Request) (page, pageSize int32) {
	page = 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = int32(v)
	}
	pageSize = defaultPageSize
	if v, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && v > 0 {
		pageSize = int32(v)
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

// maxSearchLen limita o tamanho de ?search= repassado ao patient-data-service.
const maxSearchLen = 100

// parseSearchAndGender lê ?search=&gender= da query string. search é aparado
// e truncado em maxSearchLen caracteres; gender precisa ser "", "male" ou
// "female" (ok=false caso contrário, indicando erro 400 para o chamador).
func parseSearchAndGender(r *http.Request) (search, gender string, ok bool) {
	search = strings.TrimSpace(r.URL.Query().Get("search"))
	if runes := []rune(search); len(runes) > maxSearchLen {
		search = string(runes[:maxSearchLen])
	}
	gender = r.URL.Query().Get("gender")
	switch gender {
	case "", "male", "female":
		return search, gender, true
	default:
		return search, gender, false
	}
}

// setPaginationHeaders anota a resposta com metadados de paginação. patients é
// a lista já recebida (até pageSize+1 itens); a função trunca para pageSize e
// devolve a lista pronta para ser enviada ao data-transform-service.
func setPaginationHeaders[T any](w http.ResponseWriter, page, pageSize int32, patients []T) []T {
	hasMore := len(patients) > int(pageSize)
	if hasMore {
		patients = patients[:pageSize]
	}
	w.Header().Set("X-Page", strconv.Itoa(int(page)))
	w.Header().Set("X-Page-Size", strconv.Itoa(int(pageSize)))
	w.Header().Set("X-Has-More", fmt.Sprintf("%t", hasMore))
	w.Header().Set("Access-Control-Expose-Headers", "X-Page, X-Page-Size, X-Has-More")
	return patients
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

		ctx, cancel := downstreamCtx(r)
		defer cancel()

		authResp, err := checkAuth(ctx, r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawPatient, err := pdClient.GetPatient(ctx, &pdpb.GetPatientRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar dados", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformPatient(ctx, &dtpb.TransformPatientRequest{
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

		ctx, cancel := downstreamCtx(r)
		defer cancel()

		authResp, err := checkAuth(ctx, r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawSummary, err := pdClient.GetClinicalSummary(ctx, &pdpb.GetClinicalSummaryRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar resumo", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformClinicalSummary(ctx, &dtpb.TransformClinicalSummaryRequest{
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

		ctx, cancel := downstreamCtx(r)
		defer cancel()

		_, err := checkAuth(ctx, r, authClient, "", condition)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawStats, err := pdClient.GetCohortStatistics(ctx, &pdpb.GetCohortStatisticsRequest{ConditionCode: condition})
		if err != nil {
			http.Error(w, "Erro ao buscar estatísticas", http.StatusInternalServerError)
			return
		}

		aggResponse, err := dtClient.TransformCohortStatistics(ctx, &dtpb.TransformCohortStatisticsRequest{
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

func getDoctorPatientsHandler(
	authClient pb.AuthorizationServiceClient,
	pdClient pdpb.PatientDataServiceClient,
	dtClient dtpb.DataTransformServiceClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := downstreamCtx(r)
		defer cancel()

		_, err := checkAuth(ctx, r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		page, pageSize := parsePagination(r)
		search, gender, ok := parseSearchAndGender(r)
		if !ok {
			http.Error(w, "Parâmetro gender inválido", http.StatusBadRequest)
			return
		}

		stream, err := pdClient.ListPatientsByDoctor(ctx, &pdpb.ListPatientsByDoctorRequest{
			DoctorUsername: username,
			Page:           page,
			PageSize:       pageSize,
			Search:         search,
			Gender:         gender,
		})
		if err != nil {
			http.Error(w, "Erro ao listar stream de pacientes", http.StatusInternalServerError)
			return
		}

		var patientsList []*pdpb.Patient
		for {
			patient, err := stream.Recv()
			if err == io.EOF {
				break
			}

			if err != nil {
				http.Error(w, "Erro ao ler stream de pacientes", http.StatusInternalServerError)
				return
			}
			patientsList = append(patientsList, patient)
		}
		patientsList = setPaginationHeaders(w, page, pageSize, patientsList)

		fhirResponse, err := dtClient.TransformPatientList(ctx, &dtpb.TransformPatientListRequest{
			Patients:    patientsList,
			AccessLevel: dtpb.AccessLevel_FULL,
		})

		if err != nil {
			http.Error(w, "Erro na formatação da lista", http.StatusInternalServerError)
			return
		}
		respondWithJSON(w, fhirResponse)
	}
}

func getInternPatientsHandler(
	authClient pb.AuthorizationServiceClient,
	pdClient pdpb.PatientDataServiceClient,
	dtClient dtpb.DataTransformServiceClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := downstreamCtx(r)
		defer cancel()

		_, err := checkAuth(ctx, r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		page, pageSize := parsePagination(r)
		search, gender, ok := parseSearchAndGender(r)
		if !ok {
			http.Error(w, "Parâmetro gender inválido", http.StatusBadRequest)
			return
		}

		stream, err := pdClient.ListSupervisedPatients(ctx, &pdpb.ListSupervisedPatientsRequest{
			InternUsername: username,
			Page:           page,
			PageSize:       pageSize,
			Search:         search,
			Gender:         gender,
		})
		if err != nil {
			http.Error(w, "Erro ao listar stream de pacientes supervisionados", http.StatusInternalServerError)
			return
		}

		var patientsList []*pdpb.Patient
		for {
			patient, err := stream.Recv()
			if err == io.EOF {
				break
			}

			if err != nil {
				http.Error(w, "Erro ao ler stream de pacientes", http.StatusInternalServerError)
				return
			}
			patientsList = append(patientsList, patient)
		}
		patientsList = setPaginationHeaders(w, page, pageSize, patientsList)

		fhirResponse, err := dtClient.TransformPatientList(ctx, &dtpb.TransformPatientListRequest{
			Patients:    patientsList,
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
		ctx, cancel := downstreamCtx(r)
		defer cancel()

		_, err := checkAuth(ctx, r, authClient, "", "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		username := getUsernameFromRequest(r)
		rawList, err := pdClient.ListProjectsByResearcher(ctx, &pdpb.ListProjectsByResearcherRequest{ResearcherUsername: username})
		if err != nil {
			http.Error(w, "Erro ao listar projetos de pesquisa", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformProjects(ctx, &dtpb.TransformProjectsRequest{
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

		ctx, cancel := downstreamCtx(r)
		defer cancel()

		authResp, err := checkAuth(ctx, r, authClient, patientID, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		rawPatient, err := pdClient.GetPatient(ctx, &pdpb.GetPatientRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar paciente", http.StatusInternalServerError)
			return
		}

		rawHistory, err := pdClient.GetClinicalHistory(ctx, &pdpb.GetClinicalHistoryRequest{PatientId: patientID})
		if err != nil {
			http.Error(w, "Erro ao buscar histórico", http.StatusInternalServerError)
			return
		}

		fhirResponse, err := dtClient.TransformClinicalHistory(ctx, &dtpb.TransformClinicalHistoryRequest{
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

		ctx, cancel := downstreamCtx(r)
		defer cancel()

		authResp, err := checkAuth(ctx, r, authClient, "", condition)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		page, pageSize := parsePagination(r)

		// Uma página de pacientes+exames em uma única chamada, em vez do
		// antigo stream + um ListClinicalEvents por paciente (N+1) — o que
		// esgotava o pool do patient-data-service sob carga.
		cohortResp, err := pdClient.ListCohortExams(ctx, &pdpb.ListCohortExamsRequest{
			ConditionCode: condition,
			Page:          page,
			PageSize:      pageSize,
		})
		if err != nil {
			http.Error(w, "Erro ao buscar exames da coorte", http.StatusInternalServerError)
			return
		}
		items := setPaginationHeaders(w, page, pageSize, cohortResp.GetPatients())

		patientExamsList := make([]*dtpb.PatientExams, 0, len(items))
		for _, it := range items {
			patientExamsList = append(patientExamsList, &dtpb.PatientExams{
				Patient: it.GetPatient(),
				Exams:   it.GetExams(),
			})
		}

		fhirResponse, err := dtClient.TransformCohortExams(ctx, &dtpb.TransformCohortExamsRequest{
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
