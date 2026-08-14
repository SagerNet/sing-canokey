//go:build !linux

package canokey

/*
#cgo LDFLAGS: -lcanokey-virt -lcanokey-core -lcanokey-crypto -ltfpsacrypto
*/
import "C"
