package main

import (
	"context"
	"flag"
	"github.com/go-redis/redis/v8"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v8"
	"github.com/rotisserie/eris"
	"log"
	"syscall"
	"time"
	"ws/gracefull_shutdown"
)

const mutexName = "myVariableMutex"

func main() {
	redisAddr := new(string)
	flag.StringVar(redisAddr, "redisAddr", "127.0.0.1:6379", "Redis server address")
	flag.Parse()

	gs := gracefull_shutdown.NewGracefulShutdown(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer gs.CancelContext()
	ctx := gs.GetContext()

	rdb := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: "",
		DB:       0,
	})
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatalln(eris.Wrap(err, "Can't ping Redis server"))
	}

	rs := redsync.New(goredis.NewPool(rdb))

	workerDoneCh := func() chan bool {
		done := make(chan bool)
		go func() {
			defer func() {
				gs.CancelContext()
				close(done)
			}()
			m := rs.NewMutex(mutexName,
				redsync.WithExpiry(time.Second*60),
				redsync.WithRetryDelay(time.Millisecond*500),
				redsync.WithTries(120),
			)
			if err := m.Lock(); err != nil {
				log.Println(eris.Wrapf(err, "Can't lock mutex %q", mutexName))
				return
			}
			log.Printf("Successfull lock %q", mutexName)
			defer func() {
				if _, err := m.Unlock(); err != nil {
					log.Println(eris.Wrapf(err, "Can't unlock %q", mutexName))
					return
				}
			}()
			t := time.NewTimer(time.Second * 60)
			defer t.Stop()
			select {
			case <-t.C:
				return
			case <-ctx.Done():
				return
			}
		}()
		return done
	}()
	gs.AddWaitCallback("waitWorkerDone", func(ctx context.Context) error {
		<-workerDoneCh
		return nil
	})
	<-gs.Wait(time.Second * 10)
	log.Println("Exit...")
}
