package canokey

/*
#cgo LDFLAGS: -Wl,--start-group -lcanokey-virt -lcanokey-core -lcanokey-crypto -ltfpsacrypto -Wl,--end-group -lbcrypt -lpthread
*/
import "C"
