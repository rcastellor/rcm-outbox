package config

// Topic describe un topic SNS. Sirve como esquema de configuración del stack
// y de entrada para el componente SNS.
type Topic struct {
	// Name es el nombre del topic. Para topics FIFO debe terminar en ".fifo".
	Name string `json:"name"`
	// Fifo indica si el topic es FIFO.
	Fifo bool `json:"fifo"`
	// ContentBasedDeduplication habilita la deduplicación por contenido. Si es
	// nil y el topic es FIFO, se habilita por defecto.
	ContentBasedDeduplication *bool `json:"contentBasedDeduplication"`
}
