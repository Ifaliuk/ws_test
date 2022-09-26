package main

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/nats-io/nats.go"
	"github.com/rotisserie/eris"
	"log"
	"math/rand"
	"sync"
	"syscall"
	"time"
	"ws/gracefull_shutdown"
)

func main() {
	nc, err := nats.Connect(
		"nats://127.0.0.1:4222",
		nats.Timeout(time.Second*10),
	)
	if err != nil {
		log.Fatalln(eris.Wrap(err, "Can't connect to server"))
		return
	}
	defer nc.Close()
	log.Println("Success connection to nats server...")

	gs := gracefull_shutdown.NewGracefulShutdown(syscall.SIGUSR1, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer gs.CancelContext()

	ctx := gs.GetContext()

	subj := "testSubject"

	go func() {
		inbox := nats.NewInbox()
		sub, err := nc.SubscribeSync(inbox)
		if err != nil {
			log.Println(eris.Wrapf(err, "Can't subscribe to %q", inbox))
		}
		defer func() {
			if err = sub.Unsubscribe(); err != nil {
				log.Println(eris.Wrapf(err, "Can't unsubscribe from %q", inbox))
			}
		}()
		nc.Flush()
		if err := nc.PublishRequest(subj, inbox, []byte("time")); err != nil {
			log.Println(eris.Wrap(err, "Can't publish to 'time'"))
			return
		}
		log.Printf("Success publish to %q", subj)
		var nom int
		for {
			msg, err := sub.NextMsg(2 * time.Second)
			if err != nil {
				if !eris.Is(err, nats.ErrTimeout) {
					log.Println(eris.Wrapf(err, "Can't read response from %q", inbox))
				}
				break
			}
			nom += 1
			log.Printf("Response: %s", string(msg.Data))
		}
		log.Println("Start finish application...")
		gs.CancelContext()
	}()

	const maxThreads int = 10
	var wg sync.WaitGroup
	subList := make([]*nats.Subscription, 0, maxThreads)

	wg.Add(1)
	gs.AddWaitCallback("unsubscribeNatsClients", func(ctx context.Context) error {
		defer wg.Done()
		var errRes error
		for _, sub := range subList {
			if err := sub.Unsubscribe(); err != nil {
				errRes = multierror.Append(errRes, err)
			}
		}
		return errRes
	})
	threadWithActiveConn := rand.Intn(maxThreads) + 1
	for i := 1; i <= maxThreads; i++ {
		wg.Add(1)
		go func(thread int) {
			defer wg.Done()
			sub, err := nc.Subscribe(subj, func(msg *nats.Msg) {
				var respondMsg string
				if thread == threadWithActiveConn {
					respondMsg = fmt.Sprintf("thread %d: has active connection", thread)
				} else {
					respondMsg = fmt.Sprintf("thread %d: no active connection", thread)
				}
				if err = msg.Respond([]byte(respondMsg)); err != nil {
					log.Println(eris.Wrapf(err, "Thread %d: can't respond", thread))
				}
			})
			if err != nil {
				log.Println(eris.Wrapf(err, "Thread %d: can't subscribe to %q", thread, "time"))
				return
			}
			subList = append(subList, sub)
			<-ctx.Done()
		}(i)
	}
	gs.AddWaitCallback("wgDone", func(ctx context.Context) error {
		wg.Wait()
		return nil
	})
	<-gs.Wait(time.Second * 10)
	log.Println("Application exit..")
}
