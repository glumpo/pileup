package store_test

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/store"
)

const writerEnv = "PILEUP_STORE_WRITER"

func lastHead(t tb, s *store.Store, a op.Author) op.Head {
	t.Helper()
	raws := ops(t, s, a, 1)
	if len(raws) == 0 {
		return op.Head{}
	}
	o, err := op.Decode(raws[len(raws)-1])
	if err != nil {
		t.Fatal(err)
	}
	return o.Head()
}

func TestWriterProcess(t *testing.T) {
	path := os.Getenv(writerEnv)
	if path == "" {
		t.Skip("helper process for TestInterruptedWritesLeaveConsistentStore")
	}
	s := open(t, path)
	locals := []op.Author{author(key(1)), author(key(2))}
	remote := key(3)
	v := view(t, s)
	for i, a := range locals {
		if v.VV[a] == 0 {
			create(t, s, key(byte(i+1)), join(key(byte(i+1)), "local"))
		}
	}
	head := lastHead(t, s, author(remote))
	applyRemote := func(c op.Change) {
		o, err := s.Apply(op.New(remote, pile, head, head.Lamport+50, wallTime, c))
		if err != nil {
			t.Fatal(err)
		}
		head = o.Head()
	}
	if head.Seq == 0 {
		applyRemote(join(remote, "remote"))
	}
	for i := 0; ; i++ {
		if i == 1 {
			fmt.Println("ready")
		}
		if i%3 == 2 {
			applyRemote(op.MemberSet{Name: fmt.Sprint(i)})
			continue
		}
		k := key(byte(i%2 + 1))
		id := fmt.Sprint(i % 7)
		var c op.Change = op.AssetAdd{Assets: []op.AssetMeta{asset(id, fmt.Sprint(i))}}
		if i%5 == 0 {
			c = op.AssetRemove{AssetIDs: [][]byte{[]byte(id)}}
		}
		create(t, s, k, c)
	}
}

func runAndKill(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWriterProcess$")
	cmd.Env = append(os.Environ(), writerEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() && lines.Text() != "ready" {
	}
	time.Sleep(time.Duration(rand.IntN(150)) * time.Millisecond)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
}

func TestInterruptedWritesLeaveConsistentStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pile.db")
	stored := uint64(0)
	for round := range 8 {
		runAndKill(t, path)

		s := open(t, path)
		v := view(t, s)
		if got := rebuild(t, s).View(); !reflect.DeepEqual(v, got) {
			t.Fatalf("round %d: view differs from replay of stored journal:\n got %+v\nwant %+v", round, v, got)
		}
		var total uint64
		for _, n := range v.VV {
			total += n
		}
		if total <= stored {
			t.Fatalf("round %d: writer stored nothing (%d ops)", round, total)
		}
		stored = total
		t.Logf("round %d: %d ops stored", round, total)

		local, remote := key(1), key(3)
		o := create(t, s, local, op.MemberSet{Name: "after crash"})
		if o.Seq != v.VV[author(local)]+1 || o.Lamport != v.Lamport+1 {
			t.Fatalf("round %d: new op seq %d lamport %d, journal had %d ops and L %d", round, o.Seq, o.Lamport, v.VV[author(local)], v.Lamport)
		}
		if _, err := s.Apply(op.New(remote, pile, lastHead(t, s, author(remote)), o.Lamport+1, wallTime, op.MemberSet{Name: "after crash"})); err != nil {
			t.Fatalf("round %d: next remote op rejected: %v", round, err)
		}
		stored += 2
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
