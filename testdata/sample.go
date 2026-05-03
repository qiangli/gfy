package sample

import (
	"fmt"
	"strings"
)

type Server struct {
	Name string
	Port int
}

func NewServer(name string) *Server {
	return &Server{Name: name}
}

func (s *Server) Start() {
	fmt.Println("Starting", s.Name)
	s.validate()
}

func (s *Server) validate() {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		panic("empty name")
	}
}

func Helper() {
	s := NewServer("test")
	s.Start()
}
