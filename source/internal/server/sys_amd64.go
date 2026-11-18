// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

// Package syscall has no O_PATH on amd64. This is its value in the kernel's
// asm-generic/fcntl.h, which amd64 uses.
const oPath = 0x200000
