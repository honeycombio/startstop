// Package startstop provides automatic Start/Stop for inject eliminating the
// necessity for manual ordering.
package startstop

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/facebookgo/inject"
)

// Starter defines the Start method. Objects satisfying this interface will be
// started by Start
type Starter interface {
	Start(context.Context) error
}

// Stopper defines the Stop method, objects satisfying this interface will be
// stopped by Stop.
type Stopper interface {
	Stop(context.Context) error
}

// Logger is used by Start/Stop to provide debug and error logging.
type Logger interface {
	Debugf(f string, args ...interface{})
	Errorf(f string, args ...interface{})
}

// Start starts the graph, in the right order. Start will call Start if an
// object satisfies the associated interface.
func Start(ctx context.Context, objects []*inject.Object, log Logger) error {
	levels, err := levels(objects)
	if err != nil {
		return err
	}

	for i := len(levels) - 1; i >= 0; i-- {
		level := levels[i]
		for _, o := range level {
			if starterO, ok := o.Value.(Starter); ok {

				if log != nil {
					log.Debugf("starting %s", o)
				}
				if err := starterO.Start(ctx); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Stop stops the graph, in the right order. Stop will call Stop if an
// object satisfies the associated interface. Unlike Start(), logs and
// continues if a Stop call returns an error.
func Stop(ctx context.Context, objects []*inject.Object, log Logger) error {
	levels, err := levels(objects)
	if err != nil {
		return err
	}

	for _, level := range levels {
		for _, o := range level {
			if stopperO, ok := o.Value.(Stopper); ok {
				if log != nil {
					log.Debugf("stopping %s", o)
				}
				if err := stopperO.Stop(ctx); err != nil {
					if log != nil {
						log.Errorf("error stopping %s: %s", o, err)
					}
				}
			}
		}
	}
	return nil
}

// levels returns a slice of levels of objects of the Object Graph that
// implement Start/Stop.
func levels(objects []*inject.Object) ([][]*inject.Object, error) {
	levelsMap := map[int][]*inject.Object{}

	// ensure no cycles exist for objects that need start/stop, and make a
	// flattened graph of all deps.
	for _, o := range objects {
		if !isEligible(o) {
			continue
		}

		deps := map[*inject.Object]bool{}
		paths := allPaths(o, o, deps)
		for _, p := range paths {
			// special case direct cycle to itself
			if len(p) == 1 {
				return nil, cycleError(p)
			}

			// cycle is only relevant if more than one value in the path
			// isEligible. if there's just one, there isn't really a cycle from the
			// start/stop perspective.
			count := 0
			for _, s := range p {
				if isEligible(s.Object) {
					count++
				}
			}

			if count > 1 {
				return nil, cycleError(p)
			}
		}

		startStopDeps := 0
		for dep := range deps {
			if isEligible(dep) {
				startStopDeps++
			}
		}
		levelsMap[startStopDeps] = append(levelsMap[startStopDeps], o)
	}

	var levelsMapKeys []int
	for k := range levelsMap {
		levelsMapKeys = append(levelsMapKeys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(levelsMapKeys)))

	levels := make([][]*inject.Object, 0, len(levelsMapKeys))
	for _, k := range levelsMapKeys {
		levels = append(levels, levelsMap[k])
	}
	return levels, nil
}

type path []struct {
	Field  string
	Object *inject.Object
}

type cycleError path

func (c cycleError) Error() string {
	var buf bytes.Buffer
	fmt.Fprint(&buf, "circular reference detected from")
	num := len(c)
	for _, s := range c {
		if num != 1 {
			fmt.Fprint(&buf, "\n")
		} else {
			fmt.Fprint(&buf, " ")
		}
		fmt.Fprintf(&buf, "field %s in %s", s.Field, s.Object)
	}
	if num == 1 {
		fmt.Fprint(&buf, " to itself")
	} else {
		fmt.Fprintf(&buf, "\nfield %s in %s", c[0].Field, c[0].Object)
	}
	return buf.String()
}

func allPaths(from, to *inject.Object, seen map[*inject.Object]bool) []path {
	if from != to {
		if seen[from] {
			return nil
		}
		seen[from] = true
	}

	var paths []path
	for field, value := range from.Fields {
		immediate := path{{Field: field, Object: from}}
		if value == to {
			paths = append(paths, immediate)
		} else {
			for _, p := range allPaths(value, to, seen) {
				paths = append(paths, append(immediate, p...))
			}
		}
	}
	return paths
}

func isEligible(i *inject.Object) bool {
	if _, ok := i.Value.(Starter); ok {
		return true
	}
	if _, ok := i.Value.(Stopper); ok {
		return true
	}
	return false
}
