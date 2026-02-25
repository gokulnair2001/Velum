package layers

// Layer defines the interface that all behavioral layers must implement
type Layer interface {
	// Name returns the layer's identifier
	Name() string

	// Process takes input data and returns processed output
	Process(input interface{}) (interface{}, error)
}

// ContextAwareLayer is optionally implemented by layers that process
// pipeline-level metadata (e.g., analysis context for batch configuration).
// The Pipeline checks for this interface at runtime and calls ProcessWithContext
// instead of Process when metadata is available.
type ContextAwareLayer interface {
	Layer
	// ProcessWithContext is called instead of Process when metadata is available.
	// The metadata parameter carries batch-level configuration (e.g., *behavior.AnalysisContext).
	ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error)
}

// LayerOutput captures the output of a single layer
type LayerOutput struct {
	LayerName string      `json:"layer_name"`
	Output    interface{} `json:"output"`
}

// PipelineResult contains all intermediate outputs for debugging
type PipelineResult struct {
	FinalOutput         interface{}   `json:"final_output"`
	IntermediateOutputs []LayerOutput `json:"intermediate_outputs"`
}

// Pipeline orchestrates the execution of multiple layers
type Pipeline struct {
	layers []Layer
}

// NewPipeline creates a new layer pipeline
func NewPipeline() *Pipeline {
	return &Pipeline{
		layers: make([]Layer, 0),
	}
}

// Register adds a layer to the pipeline
func (p *Pipeline) Register(layer Layer) {
	p.layers = append(p.layers, layer)
}

// Execute runs all layers in sequence
func (p *Pipeline) Execute(input interface{}) (interface{}, error) {
	current := input

	for _, layer := range p.layers {
		output, err := layer.Process(current)
		if err != nil {
			return nil, err
		}
		current = output
	}

	return current, nil
}

// ExecuteWithDebug runs all layers and captures intermediate outputs
func (p *Pipeline) ExecuteWithDebug(input interface{}) (*PipelineResult, error) {
	current := input
	intermediates := make([]LayerOutput, 0, len(p.layers))

	for _, layer := range p.layers {
		output, err := layer.Process(current)
		if err != nil {
			return nil, err
		}

		intermediates = append(intermediates, LayerOutput{
			LayerName: layer.Name(),
			Output:    output,
		})

		current = output
	}

	return &PipelineResult{
		FinalOutput:         current,
		IntermediateOutputs: intermediates,
	}, nil
}

// LayerNames returns the names of all registered layers
func (p *Pipeline) LayerNames() []string {
	names := make([]string, len(p.layers))
	for i, layer := range p.layers {
		names[i] = layer.Name()
	}
	return names
}

// ExecuteWithContext runs all layers, passing metadata to context-aware layers.
// Layers implementing ContextAwareLayer receive metadata via ProcessWithContext.
// Other layers use their standard Process method. This allows batch-level
// configuration (like AnalysisContext) to reach layers that need it without
// modifying layers that don't.
func (p *Pipeline) ExecuteWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	current := input

	for _, layer := range p.layers {
		var output interface{}
		var err error

		if metadata != nil {
			if cal, ok := layer.(ContextAwareLayer); ok {
				output, err = cal.ProcessWithContext(current, metadata)
			} else {
				output, err = layer.Process(current)
			}
		} else {
			output, err = layer.Process(current)
		}

		if err != nil {
			return nil, err
		}
		current = output
	}

	return current, nil
}

// ExecuteWithContextAndDebug runs all layers with metadata, capturing intermediate outputs.
func (p *Pipeline) ExecuteWithContextAndDebug(input interface{}, metadata interface{}) (*PipelineResult, error) {
	current := input
	intermediates := make([]LayerOutput, 0, len(p.layers))

	for _, layer := range p.layers {
		var output interface{}
		var err error

		if metadata != nil {
			if cal, ok := layer.(ContextAwareLayer); ok {
				output, err = cal.ProcessWithContext(current, metadata)
			} else {
				output, err = layer.Process(current)
			}
		} else {
			output, err = layer.Process(current)
		}

		if err != nil {
			return nil, err
		}

		intermediates = append(intermediates, LayerOutput{
			LayerName: layer.Name(),
			Output:    output,
		})

		current = output
	}

	return &PipelineResult{
		FinalOutput:         current,
		IntermediateOutputs: intermediates,
	}, nil
}
