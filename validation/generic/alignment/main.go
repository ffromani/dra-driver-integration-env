/*
 * Copyright 2024 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ffromani/ctrreschk/pkg/align"
	"github.com/ffromani/ctrreschk/pkg/environ"
	"github.com/ffromani/ctrreschk/pkg/machine"
	"github.com/ffromani/ctrreschk/pkg/resources"
)

func main() {
	env := environ.New()

	container, err := resources.Discover(env)
	if err != nil {
		log.Fatalf("ERR: resource discover failed: %v", err)
	}
	machine, err := machine.Discover(env)
	if err != nil {
		log.Fatalf("ERR: machine discover failed: %v", err)
	}
	result, err := align.Check(env, container, machine)
	if err != nil {
		log.Fatalf("ERR: alignment check failed: %v", err)
	}
	err = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		log.Fatalf("ERR: response encoding failed: %v", err)
	}

	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, syscall.SIGINT, syscall.SIGTERM)
	<-exitSignal
}
