package config

import (
	"errors"
	"os"
)

// DBConfig agrupa la configuración de conexión a la base de datos leída del
// entorno, común a todas las Lambdas.
type DBConfig struct {
	// DBSecretARN es el ARN del secreto de Secrets Manager que contiene las
	// credenciales de PostgreSQL.
	DBSecretARN string
}

// LoadDB lee la configuración de conexión a la base de datos desde las
// variables de entorno.
func LoadDB() (*DBConfig, error) {
	arn := os.Getenv("DB_SECRET_ARN")
	if arn == "" {
		return nil, errors.New("variable de entorno DB_SECRET_ARN no definida")
	}
	return &DBConfig{DBSecretARN: arn}, nil
}
