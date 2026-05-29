/*
 * Copyright 2025 Cong Wang
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
	"errors"
	"fmt"
	"os"
)

func main() {
	root := buildRootCmd(newClient())
	if err := root.Execute(); err != nil {
		// Command handlers report their own errors to stderr and return
		// errExit; anything else is an error cobra surfaced (e.g. unknown
		// command or flag) that we still need to print.
		if !errors.Is(err, errExit) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
