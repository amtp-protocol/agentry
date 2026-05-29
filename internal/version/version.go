/*
 * Copyright 2026 Cong Wang
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

// Package version exposes the build version shared by all agentry binaries.
//
// Version defaults to "dev" and is overridden at build time via the linker:
//
//	-ldflags "-X github.com/amtp-protocol/agentry/internal/version.Version=v0.1.0"
//
// The Makefile derives the value from `git describe --tags --always --dirty`.
package version

// Version is the build version. It is "dev" for plain `go build`/`go run`
// invocations and is set by the linker for release builds.
var Version = "dev"
