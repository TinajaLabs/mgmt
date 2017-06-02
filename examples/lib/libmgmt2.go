// libmgmt example
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/purpleidea/mgmt/gapi"
	mgmt "github.com/purpleidea/mgmt/lib"
	"github.com/purpleidea/mgmt/pgraph"
	"github.com/purpleidea/mgmt/resources"
)

// MyGAPI implements the main GAPI interface.
type MyGAPI struct {
	Name     string // graph name
	Count    uint   // number of resources to create
	Interval uint   // refresh interval, 0 to never refresh

	data        gapi.Data
	initialized bool
	closeChan   chan struct{}
	wg          sync.WaitGroup // sync group for tunnel go routines
}

// NewMyGAPI creates a new MyGAPI struct and calls Init().
func NewMyGAPI(data gapi.Data, name string, interval uint, count uint) (*MyGAPI, error) {
	obj := &MyGAPI{
		Name:     name,
		Count:    count,
		Interval: interval,
	}
	return obj, obj.Init(data)
}

// Init initializes the MyGAPI struct.
func (obj *MyGAPI) Init(data gapi.Data) error {
	if obj.initialized {
		return fmt.Errorf("already initialized")
	}
	if obj.Name == "" {
		return fmt.Errorf("the graph name must be specified")
	}
	obj.data = data // store for later
	obj.closeChan = make(chan struct{})
	obj.initialized = true
	return nil
}

// Graph returns a current Graph.
func (obj *MyGAPI) Graph() (*pgraph.Graph, error) {
	if !obj.initialized {
		return nil, fmt.Errorf("libmgmt: MyGAPI is not initialized")
	}

	g, err := pgraph.NewGraph(obj.Name)
	if err != nil {
		return nil, err
	}
	var vertex pgraph.Vertex
	for i := uint(0); i < obj.Count; i++ {
		n := &resources.NoopRes{
			BaseRes: resources.BaseRes{
				Name:       fmt.Sprintf("noop%d", i),
				MetaParams: resources.DefaultMetaParams,
			},
		}
		g.AddVertex(n)
		if i > 0 {
			g.AddEdge(vertex, n, &resources.Edge{Name: fmt.Sprintf("e%d", i)})
		}
		vertex = n // save
	}

	//g, err := config.NewGraphFromConfig(obj.data.Hostname, obj.data.World, obj.data.Noop)
	return g, nil
}

// Next returns nil errors every time there could be a new graph.
func (obj *MyGAPI) Next() chan gapi.Next {
	ch := make(chan gapi.Next)
	obj.wg.Add(1)
	go func() {
		defer obj.wg.Done()
		defer close(ch) // this will run before the obj.wg.Done()
		if !obj.initialized {
			next := gapi.Next{
				Err:  fmt.Errorf("libmgmt: MyGAPI is not initialized"),
				Exit: true, // exit, b/c programming error?
			}
			ch <- next
		}
		startChan := make(chan struct{}) // start signal
		close(startChan)                 // kick it off!

		ticker := make(<-chan time.Time)
		if obj.data.NoStreamWatch || obj.Interval <= 0 {
			ticker = nil
		} else {
			// arbitrarily change graph every interval seconds
			t := time.NewTicker(time.Duration(obj.Interval) * time.Second)
			defer t.Stop()
			ticker = t.C
		}
		for {
			select {
			case <-startChan: // kick the loop once at start
				startChan = nil // disable
				// pass
			case <-ticker:
				// pass
			case <-obj.closeChan:
				return
			}

			log.Printf("libmgmt: Generating new graph...")
			select {
			case ch <- gapi.Next{}: // trigger a run
			case <-obj.closeChan:
				return
			}
		}
	}()
	return ch
}

// Close shuts down the MyGAPI.
func (obj *MyGAPI) Close() error {
	if !obj.initialized {
		return fmt.Errorf("libmgmt: MyGAPI is not initialized")
	}
	close(obj.closeChan)
	obj.wg.Wait()
	obj.initialized = false // closed = true
	return nil
}

// Run runs an embedded mgmt server.
func Run(count uint) error {

	obj := &mgmt.Main{}
	obj.Program = "libmgmt" // TODO: set on compilation
	obj.Version = "0.0.1"   // TODO: set on compilation
	obj.TmpPrefix = true
	obj.IdealClusterSize = -1
	obj.ConvergedTimeout = -1
	obj.Noop = true

	obj.GAPI = &MyGAPI{ // graph API
		Name:     "libmgmt", // TODO: set on compilation
		Count:    count,     // number of vertices to add
		Interval: 15,        // arbitrarily change graph every 15 seconds
	}

	if err := obj.Init(); err != nil {
		return err
	}

	// install the exit signal handler
	exit := make(chan struct{})
	defer close(exit)
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt) // catch ^C
		//signal.Notify(signals, os.Kill) // catch signals
		signal.Notify(signals, syscall.SIGTERM)

		select {
		case sig := <-signals: // any signal will do
			if sig == os.Interrupt {
				log.Println("Interrupted by ^C")
				obj.Exit(nil)
				return
			}
			log.Println("Interrupted by signal")
			obj.Exit(fmt.Errorf("killed by %v", sig))
			return
		case <-exit:
			return
		}
	}()

	if err := obj.Run(); err != nil {
		return err
	}
	return nil
}

func main() {
	log.Printf("Hello!")
	var count uint = 1 // default
	if len(os.Args) == 2 {
		if i, err := strconv.Atoi(os.Args[1]); err == nil && i > 0 {
			count = uint(i)
		}
	}
	if err := Run(count); err != nil {
		fmt.Println(err)
		os.Exit(1)
		return
	}
	log.Printf("Goodbye!")
}
