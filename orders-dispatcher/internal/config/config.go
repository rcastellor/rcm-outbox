package config

import (
	"errors"
	"os"
	"strconv"

	platformconfig "github.com/rcastellor/rcm-outbox/rcm-platform/config"
)

const defaultBatchSize = 10
const defaultMaxWorkers = 20

// Config agrupa la configuración de la Lambda del dispatcher leída del entorno.
type Config struct {
	// DB es la configuración de conexión a la base de datos (compartida con el
	// resto de Lambdas vía rcm-platform).
	DB *platformconfig.DBConfig
	// DispatchQueueURL es la URL de la cola SQS donde el dispatcher encola los
	// trabajos de outbox.
	DispatchQueueURL string
	// BatchSize es el tamaño de bloque de registros que procesa cada worker;
	// sirve para calcular cuántos workers son necesarios.
	BatchSize int
	// MaxWorkers es el tope de instancias de worker que el dispatcher lanza por
	// invocación.
	MaxWorkers int
}

// Load lee la configuración desde las variables de entorno.
func Load() (*Config, error) {
	db, err := platformconfig.LoadDB()
	if err != nil {
		return nil, err
	}

	queueURL := os.Getenv("DISPATCH_QUEUE_URL")
	if queueURL == "" {
		return nil, errors.New("variable de entorno DISPATCH_QUEUE_URL no definida")
	}

	batchSize, err := positiveInt("BATCH_SIZE", defaultBatchSize)
	if err != nil {
		return nil, err
	}

	maxWorkers, err := positiveInt("MAX_WORKERS", defaultMaxWorkers)
	if err != nil {
		return nil, err
	}

	return &Config{
		DB:               db,
		DispatchQueueURL: queueURL,
		BatchSize:        batchSize,
		MaxWorkers:       maxWorkers,
	}, nil
}

// positiveInt lee una variable de entorno entera positiva, devolviendo el
// default si no está definida.
func positiveInt(name string, def int) (int, error) {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, errors.New("variable de entorno " + name + " inválida")
		}
		return n, nil
	}
	return def, nil
}
