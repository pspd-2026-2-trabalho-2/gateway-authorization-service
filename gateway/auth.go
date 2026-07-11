package main

import (
	"context"
	"fmt"
	pb "gateway-auth-service/proto"
	dtpb "gateway-auth-service/proto/datatransform/v1"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// validRoles são as únicas roles do realm Keycloak reconhecidas pelo gateway.
var validRoles = map[string]bool{"MEDICO": true, "ESTAGIARIO": true, "PESQUISADOR": true}

var jwks keyfunc.Keyfunc
var keycloakIssuer string

// parseClaims extrai e valida o access_token (RS256/JWKS) do header Authorization.
func parseClaims(r *http.Request) (jwt.MapClaims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("token não fornecido")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, err := jwt.Parse(tokenString, jwks.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(keycloakIssuer),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("token inválido")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("token inválido")
	}
	return claims, nil
}

// usernameAndRole extrai preferred_username e a primeira role reconhecida em realm_access.roles.
func usernameAndRole(claims jwt.MapClaims) (username string, role string) {
	username, _ = claims["preferred_username"].(string)

	realmAccess, _ := claims["realm_access"].(map[string]interface{})
	roles, _ := realmAccess["roles"].([]interface{})
	for _, r := range roles {
		roleStr, ok := r.(string)
		if ok && validRoles[roleStr] {
			return username, roleStr
		}
	}
	return username, ""
}

func mapAccessLevel(authLevel pb.AccessLevel) dtpb.AccessLevel {
	switch authLevel {
	case pb.AccessLevel_FULL:
		return dtpb.AccessLevel_FULL
	case pb.AccessLevel_PARTIAL:
		return dtpb.AccessLevel_PARTIAL
	case pb.AccessLevel_ANONYMIZED:
		return dtpb.AccessLevel_ANONYMIZED
	case pb.AccessLevel_AGGREGATED:
		return dtpb.AccessLevel_AGGREGATED
	default:
		return dtpb.AccessLevel_ACCESS_LEVEL_UNSPECIFIED
	}
}

func checkAuth(r *http.Request, authClient pb.AuthorizationServiceClient, targetPatientID string, projectID string) (*pb.AuthorizeResponse, error) {
	claims, err := parseClaims(r)
	if err != nil {
		return nil, err
	}
	username, role := usernameAndRole(claims)

	authResp, err := authClient.Authorize(context.Background(), &pb.AuthorizeRequest{
		Username:        username,
		Role:            role,
		TargetPatientId: targetPatientID,
		ProjectId:       projectID,
	})

	if err != nil {
		return nil, fmt.Errorf("erro no serviço de autorização: %v", err)
	}
	if authResp.Decision == pb.Decision_DENY {
		return nil, fmt.Errorf("acesso negado: %s", authResp.Message)
	}

	return authResp, nil
}
