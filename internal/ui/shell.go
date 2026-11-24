package ui

import (
	"io"
	"os/exec"
)

// Shell runs cmd on the frontend's terminal; before and after let the
// frontend hand the terminal over in its stock state and take it back.
type Shell struct {
	Cmd           *exec.Cmd
	Before, After func(w io.Writer)
	out           io.Writer
}

func (s *Shell) SetStdin(r io.Reader)  { s.Cmd.Stdin = r }
func (s *Shell) SetStdout(w io.Writer) { s.Cmd.Stdout, s.out = w, w }
func (s *Shell) SetStderr(w io.Writer) { s.Cmd.Stderr = w }

func (s *Shell) Run() error {
	if s.Before != nil && s.out != nil {
		s.Before(s.out)
	}
	err := s.Cmd.Run()
	if s.After != nil && s.out != nil {
		s.After(s.out)
	}
	return err
}
