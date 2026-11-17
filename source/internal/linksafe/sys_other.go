// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

//go:build !amd64

package linksafe

import "syscall"

const sysRenameat2 = syscall.SYS_RENAMEAT2
