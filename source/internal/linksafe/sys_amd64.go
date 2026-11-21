// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package linksafe

// Package syscall stopped at an older table on amd64.
const sysRenameat2 = 316

// Nor has it O_PATH there. This is the kernel's generic value.
const oPath = 0x200000
