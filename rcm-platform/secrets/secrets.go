package secrets

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// DBCredentials es el documento JSON almacenado en Secrets Manager con las
// credenciales de PostgreSQL. Coincide con el esquema creado por infra.
type DBCredentials struct {
	Engine   string `json:"engine"`
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DbName   string `json:"dbname"`
}

// Fetch recupera las credenciales de PostgreSQL desde Secrets Manager.
func Fetch(ctx context.Context, secretARN string) (*DBCredentials, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("cargando configuración AWS: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretARN,
	})
	if err != nil {
		return nil, fmt.Errorf("obteniendo secreto %s: %w", secretARN, err)
	}
	if out.SecretString == nil {
		return nil, fmt.Errorf("el secreto %s no tiene SecretString", secretARN)
	}

	var creds DBCredentials
	if err := json.Unmarshal([]byte(*out.SecretString), &creds); err != nil {
		return nil, fmt.Errorf("parseando credenciales del secreto: %w", err)
	}
	return &creds, nil
}

// DSN construye el DSN de PostgreSQL a partir de las credenciales.
func (c *DBCredentials) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.Username, c.Password, c.Host, c.Port, c.DbName)
}
