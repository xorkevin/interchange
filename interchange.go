package interchange

type (
	Interchange interface {
		Forward(port int, dest string)
	}

	server struct {
		verbose bool
	}
)

func NewInterchange(verbose bool) Interchange {
	return &server{
		verbose: verbose,
	}
}

func (s *server) Forward(port int, dest string) {
}
