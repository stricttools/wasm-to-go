package reserve

import "syscall"

// Linux counts private writable mappings against its commit limit unless
// they are mapped with MAP_NORESERVE.
const noreserveFlag = syscall.MAP_NORESERVE
