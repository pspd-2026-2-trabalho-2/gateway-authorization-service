package main

import (
	"context"
	"fmt"
	pb "gateway-auth-service/proto"
	dtpb "gateway-auth-service/proto/datatransform/v1"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret []byte

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
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("token não fornecido")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("token inválido")
	}

	claims := token.Claims.(jwt.MapClaims)
	username, _ := claims["username"].(string)
	role, _ := claims["role"].(string)

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