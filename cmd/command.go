package cmd

import (
	"fmt"
	"launchpad.net/gnuflag"
	"launchpad.net/juju/go/log"
	"os"
	"strings"
)

// Info holds everything necessary to describe a Command's intent and usage.
type Info struct {
	// Name is the Command's name.
	Name string

	// Usage describes the format of a valid call to the Command.
	Usage string

	// Purpose is a short explanation of the Command's purpose.
	Purpose string

	// Doc is the long documentation for the Command.
	Doc string
}

// NewInfo returns an Info whose Usage is constructed from name and args,
// to ensure that Usage matches Name.
func NewInfo(name string, args string, purpose string, doc string) *Info {
	usage := fmt.Sprintf("%s %s", name, args)
	return &Info{name, usage, purpose, doc}
}

// Command is implemented by types that interpret any command-line arguments
// passed to the "juju" command.
type Command interface {
	// Info returns information about the command.
	Info() *Info

	// InitFlagSet prepares a FlagSet such that Parse~ing that FlagSet will
	// initialize the Command's options.
	InitFlagSet(f *gnuflag.FlagSet)

	// ParsePositional is called by Parse to allow the Command to handle
	// positional command-line arguments.
	ParsePositional(args []string) error

	// Run will execute the command according to the options and positional
	// arguments interpreted by a call to Parse.
	Run() error
}

// NewFlagSet returns a FlagSet initialized for use with c.
func NewFlagSet(c Command) *gnuflag.FlagSet {
	f := gnuflag.NewFlagSet(c.Info().Name, gnuflag.ExitOnError)
	f.Usage = func() { PrintUsage(c) }
	c.InitFlagSet(f)
	return f
}

// PrintUsage prints usage information for c to stderr.
func PrintUsage(c Command) {
	i := c.Info()
	fmt.Fprintf(os.Stderr, "usage: %s\n", i.Usage)
	fmt.Fprintf(os.Stderr, "purpose: %s\n", i.Purpose)
	fmt.Fprintf(os.Stderr, "\noptions:\n")
	NewFlagSet(c).PrintDefaults()
	if i.Doc != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", strings.TrimSpace(i.Doc))
	}
}

// Parse parses args on c. This must be called before c is Run.
// If intersperse is true, flags and positional arguments
// are allowed to be mixed. Otherwise, everything following
// the first non-flag is handled as a positional argument.
func Parse(c Command, intersperse bool, args []string) error {
	f := NewFlagSet(c)
	if err := f.Parse(intersperse, args); err != nil {
		return err
	}
	return c.ParsePositional(f.Args())
}

// CheckEmpty is a utility function that returns an error if args is not empty.
func CheckEmpty(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unrecognised args: %s", args)
	}
	return nil
}

// Main will Parse and Run a Command, and exit appropriately.
func Main(c Command, args []string) {
	if err := Parse(c, false, args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		PrintUsage(c)
		os.Exit(2)
	}
	if err := c.Run(); err != nil {
		log.Debugf("%s command failed: %s\n", c.Info().Name, err)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
