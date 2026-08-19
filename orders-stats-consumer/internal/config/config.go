package config

import (
	"errors"
	"os"
)

// Config agrupa la configuración de la Lambda consumidora de estadísticas
// leída del entorno.
type Config struct {
	// StatsTableName es el nombre de la tabla DynamoDB donde se agregan los
	// items comprados por cliente, producto y día.
	StatsTableName string
}

// Load lee la configuración desde las variables de entorno.
func Load() (*Config, error) {
	table := os.Getenv("STATS_TABLE_NAME")
	if table == "" {
		return nil, errors.New("variable de entorno STATS_TABLE_NAME no definida")
	}

	return &Config{StatsTableName: table}, nil
}
