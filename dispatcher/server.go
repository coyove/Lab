package main

import (
	"encoding/binary"
	"flag"
	"hash/fnv"
	"log"
	"net/http"
	"time"

	"github.com/boltdb/bolt"
)

const DEFAULT_WEIGHT = 1

var listen = flag.String("l", ":8999", "")
var password = flag.String("p", "disz", "")
var db *bolt.DB

func greet(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Password") != *password {
		w.WriteHeader(400)
		return
	}
	u := r.Header.Get("Url")
	if u == "" {
		w.WriteHeader(400)
		return
	}

	h := fnv.New64()
	h.Write([]byte(u))
	key := [8]byte{}
	binary.BigEndian.PutUint64(key[:], h.Sum64())

	log.Println("ask", h.Sum64(), u)

	action := 'a'
	now := uint32(time.Now().Unix()) & 0xffffff00
	db.Update(func(tx *bolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte("main"))
		p := b.Get(key[:])
		if len(p) != 8 {
			action = 'c'
			p := [8]byte{}
			binary.BigEndian.PutUint32(p[:4], now)
			p[3] = DEFAULT_WEIGHT
			binary.BigEndian.PutUint32(p[4:], 1)
			b.Put(key[:], p[:])

			log.Println(u, "newly added")
			return nil
		}

		tsw := binary.BigEndian.Uint32(p[:4])
		hits := binary.BigEndian.Uint32(p[4:])
		ts := tsw & 0xffffff00

		xp := [8]byte{}
		copy(xp[:], p)

		hits++
		if now-ts > 86272 {
			action = 'c'
			w := xp[3]
			binary.BigEndian.PutUint32(xp[:4], now)
			xp[3] = w
		} else {
			action = 'i'
			log.Println(u, "no update required")
		}

		binary.BigEndian.PutUint32(xp[4:], hits)
		b.Put(key[:], xp[:])
		return nil
	})

	if action == 'c' {
		w.WriteHeader(200)
	} else if action == 'i' {
		w.WriteHeader(304)
	} else {
		w.WriteHeader(400)
	}
}

func main() {
	flag.Parse()
	db, _ = bolt.Open("data.db", 0777, nil)
	http.HandleFunc("/", greet)
	log.Println("listen on", *listen)
	http.ListenAndServe(*listen, nil)
}
