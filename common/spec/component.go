/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package spec

type Component interface {
	Start() error
	Stop()
	Running() bool
}
