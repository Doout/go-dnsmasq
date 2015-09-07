// Copyright (c) 2015 Jan Broer
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

// Package hosts provides address lookups from local hostfile (usually /etc/hosts).
package hosts

import (
	"io/ioutil"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Config stores options for hostsfile
type Config struct {
	// Positive value enables polling
	Poll    int
	Verbose bool
}

// Hostsfile represents a file containing hosts
type Hostsfile struct {
	config *Config
	hosts  *hostlist
	file   struct {
		size  int64
		path  string
		mtime time.Time
	}
	hostMutex sync.RWMutex
}

// NewHostsfile returns a new Hostsfile object
func NewHostsfile(path string, config *Config) (*Hostsfile, error) {
	h := Hostsfile{config: config}
	// when no hostfile is given we return an empty hostlist
	if path == "" {
		h.hosts = new(hostlist)
		return &h, nil
	}

	h.file.path = path
	if err := h.loadHostEntries(); err != nil {
		return nil, err
	}

	if h.config.Poll > 0 {
		go h.monitorHostEntries(h.config.Poll)
	}

	if h.config.Verbose {
		log.Printf("Found entries in %s:\n", h.file.path)
		for _, hostname := range *h.hosts {
			log.Printf("%s %s \n",
				hostname.domain,
				hostname.ip.String())
		}
	}

	return &h, nil
}

func (h *Hostsfile) FindHosts(name string) (addrs []net.IP, err error) {
	name = strings.TrimSuffix(name, ".")
	h.hostMutex.RLock()
	defer h.hostMutex.RUnlock()

	for _, hostname := range *h.hosts {
		if hostname.domain == name {
			addrs = append(addrs, hostname.ip)
		}
	}

	return
}

func (h *Hostsfile) FindReverse(name string) (host string, err error) {
	h.hostMutex.RLock()
	defer h.hostMutex.RUnlock()

	for _, hostname := range *h.hosts {
		if r, _ := dns.ReverseAddr(hostname.ip.String()); name == r {
			host = dns.Fqdn(hostname.domain)
			break
		}
	}
	return
}

func (h *Hostsfile) loadHostEntries() error {
	data, err := ioutil.ReadFile(h.file.path)
	if err != nil {
		return err
	}

	h.hostMutex.Lock()
	h.hosts = newHostlist(data)
	h.hostMutex.Unlock()

	return nil
}

func (h *Hostsfile) monitorHostEntries(poll int) {
	hf := h.file

	if hf.path == "" {
		return
	}

	t := time.Duration(poll) * time.Second

	for _ = range time.Tick(t) {
		//log.Printf("go-dnsmasq: checking %q for updates…", hf.path)

		mtime, size, err := hostsFileMetadata(hf.path)
		if err != nil {
			log.Printf("go-dnsmasq: error stating hostsfile: %s", err)
			continue
		}

		if hf.mtime.Equal(mtime) && hf.size == size {
			continue // no updates
		}

		if err := h.loadHostEntries(); err != nil {
			log.Printf("go-dnsmasq: error opening hostsfile: %s", err)
		}

		if h.config.Verbose {
			log.Printf("go-dnsmasq: reloaded changed hostsfile")
		}

		h.hostMutex.Lock()
		h.file.mtime = mtime
		h.file.size = size
		hf = h.file
		h.hostMutex.Unlock()
	}
}