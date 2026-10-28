# Experimental C6 shared-owner profile

`c6-shared-v2-lab1` is an explicit schema2 software experiment. It is not the
default target, a deployment, a production-admission decision, or evidence of
physical durability. All physical/security gates remain **NOT_RUN**. This branch
is local only. Do not select, reset, flash, erase or otherwise operate devices.

The journal, recovery, namespace, quota, race and rooted native image checks pass.
**The profile is NOT ACCEPTED:** its **12 KiB internal peak memory** qualification
is unresolved, and the selected consumer independently exceeds its fixed quotas.
Object size or a passing software model is not a substitute for either gate.

## One owner, two views

`settings.OpenSharedV2(driver)` requires the explicit shared-profile target and
never formats or migrates. `ThreadSettings()` supplies `platform.Settings`;
`MatterBlobs()` supplies exact-string `Get/Set/Delete/Keys(prefix)/Close` without
any GoMatter dependency. `Limits()` returns an immutable value.

The same native lease and Radio own both views. The application must stop both
consumers and quiesce/disable the Radio before `owner.Close()` releases that
lease. Closing a view never releases it. Shared Thread `Wipe` returns
`ErrSharedScope`; Matter cleanup enumerates/deletes Matter keys only.
There is no namespace wipe or multi-key transaction.

Matter keys are nonempty valid UTF-8, no NUL, maximum 96 encoded bytes.
They are compared as exact bytes without normalization, hashing or a numeric
catalog. Prefix enumeration is deterministic and byte-exact, including partial
UTF-8 prefixes. Thread retains every uint16 key identity and list/index rules.
Missing Matter values return `ErrBlobNotFound`; genuine missing Thread data
returns `platform.ErrSettingsNotFound`. Empty blobs exist and are not missing.

Set borrows immutable caller data only until it returns, then retains flash
references, not the input. Get returns one exact-sized caller-owned blob;
Keys returns only a caller-owned slice and copied keys. Those outputs, retained
application JSON/crypto/PASE state and task stacks are not the internal owner
budget. Synchronous backend call-stack usage still is.

## Closed layout and quotas

| Property | Experimental value |
|---|---:|
| 4 MiB physical partition | `0x3d0000..0x400000`, 196,608 bytes |
| Banks | 2 x 98,304 bytes / 24 sectors |
| Header / directory | 128 / 128 x 32 bytes |
| Arena start / retirement offset | 4,224 / 98,300 |
| Arena capacity per bank | 94,076 bytes |
| Matter | 32 keys/singleton values; 65,536 encoded entry bytes; 16,384/value |
| Thread | 16 keys / 32 values; 4,096 encoded entry bytes; 512/value |
| Maximum canonical snapshot | 69,640 bytes |
| Maximum complete transaction payload | 16,488 bytes |
| Small-write reserve after a cold mutation | 64 slots plus 4,096 arena bytes |

Quotas cannot borrow from another namespace. A small write has an entire encoded
payload <=64 bytes; a 49-byte Thread counter replacement occupies 60 aligned
arena bytes. At hard maxima, snapshot + largest append + 64 x 64 is 90,224 bytes,
leaving 3,852 arena bytes. Its descriptor is already in the directory.

Use only `targets/esp32c6-shared-v2-lab1.json`. The bundled profile linker,
closed C wrapper selection, Go constants, image verifier and
`c6-shared-v2-lab1.profile.json` must agree. Legacy APIs/checkers remain schema1-
only and reject this layout. The default target and schema1 journal protocol
are unchanged. The reservations overlap: these are mutually exclusive layouts,
not two independent stores. Never flash an incompatible image over credentials.

`FormatEmptySharedV2` is a separately authorized lab-provisioning API, not an
Open side effect. It requires a disabled Radio and a streamed erased check of
**every** partition byte, including the legacy tail. It does not erase, recover
corruption, reinterpret schema1, or migrate anything.

## Atomicity and fail-closed behavior

One fixed index holds exact keys and flash offset/length references. Recovery
validates banks sequentially through that one index. Reads revalidate committed
backing. There is no partition buffer, state map or second index/value clone.

Records atomically replace a complete blob/list or tombstone. Descriptor/
payload readback and CRC precede the exact-zero record commit. COW computes
the current canonical snapshot CRC, streams it into the inactive bank while
matching that CRC, adds the pending record, validates, then commits the bank.
The old bank's last word is retired before acknowledgement. Erase sectors run
in increasing order; no uint64 generation/epoch wraps.

Dirty uncommitted tails force compaction. Partial commits/retirement, bad CRC/
profile/namespace/EUI, contradictory chains and read errors fail closed.
Any uncertain mutation latches the entire owner and both views until reopen.
Capacity refusal happens before mutation. The application must stop accepting
secured work on errors from either view; a consumer's cached reservation is
not an automatic cross-consumer error notification.

Flash uses the same nondeadlocking Radio quiescence: accepted TX and automatic
ACK finish in hardware, without waiting for a callback queued on the calling
Thread loop. The shared audited SRAM/ROM bridge retains strict geometry/guard,
page/word/sector bounds, status/completion and cache/interrupt order. The proof
must close over every extracted SRAM helper and its relocations.

## Validation and unclosed gates

The sanitized 225-input fixture preserves serializer source/tree/SHA256 and
only key byte lengths plus serialized value sizes from `without_adapter_identity`.
Tests independently recompute selected floors: 60,042 Matter entry bytes,
599 Thread entry bytes, 60,649 snapshot bytes, 9,220 largest aligned transaction,
9,186 largest value and 39-byte largest key. These are selected examples, not
all source-admitted Matter states. No quotas grow automatically for a later
counterexample; application admission remains a separate activation gate.

A subsequent **measured activation blocker** exists at GoMatter source
`0d3dd54fc1ccefd090dd0b329d9ce787abac523d`, tree
`a9221da52c6b100c58fb8a39d3f2f51e5d458d1d`: selected Root0 + OnOffLight1,
the source worker's five-fabric verified `400B-chain` fixture and 33 admitted Scenes produce
a 22,996-byte group blob and 87,357 Matter entry bytes with 19 keys. Both exceed
this fixed profile. The focused refusal regression must preserve the previous
blob and Thread reservations without mutation; it does not shard that value,
invent new admission limits, or make a consumer's multi-key sequence atomic.
The exact constructor inventory is six NV keys. That constructor currently
selects Ethernet commissioning, not a validated functional Thread admission.
The separate 12,288-byte certificate-chain acceptance gap is not a conformant
Thread activation result. Application format/admission decisions remain external;
this profile cannot activate the selected consumer unmodified.

After an explicit compiler-slot release, the focused tests, existing schema1
regressions, host cross-builds, target ELF proofs, local module ZIP/vendor check
and verified compiling mutants must pass. `tools/check-shared-image` requires
the exact manifest and checks the closed new image; `tools/check-shared-consumer`
uses a local file-only proxy and cached pinned dependencies, not publication
or a network fallback. `examples/shared-v2-link` is **link-only; never flash**.
Its proof build uses `targets/esp32c6-shared-v2-proof.json` with the explicit
`--undefined=c6_shared_v2_link_witness` linker root. The ordinary application
profile does not require that diagnostic symbol. `runtime.KeepAlive` alone and
an exported witness without this linker root were both observed to lose native
entrypoints through GC; those stripped images must be rejected, never counted
as native proofs. The negative KeepAlive source fixture is retained in testdata.

### Quantified unresolved internal memory gate

The actual C6 allocation diagnostic reports a 7,992-byte owner and 4-byte
native medium; host `unsafe.Sizeof` is 8,032 and is not the target measurement.
TinyGo 0.42.0 conservative GC adds a 4-byte object header and rounds to 16-byte
blocks: those two objects occupy at least 8,000 + 16 = **8,016 bytes**.
The remaining allowance is **at most 4,272 bytes**, before the rest of the
metadata/statics and the complete synchronous stack. The existing physical
lease also appears in target SRAM as 8 bytes plus the 1-byte `owned` flag;
it is shared with schema1, not duplicated. Shared GC metadata, merged error/
runtime statics and any other overhead are not fully attributed by that subtotal.

Actual ELF DWARF reports individual frames including `readSharedBankState`
368 bytes, the bound `commitWork` 256, `MatterBlobs.Keys` 224, and `loadShared`
176. These are **not a summed synchronous maximum**. TinyGo's `-print-stacks`
fails on this target with `unknown architecture: EM_RISCV`. External ROM
callee bodies are absent from this firmware ELF; interface dispatch, the
bound callback, runtime/allocator call chains and interrupt/preemption effects
also lack a complete upper bound. Call roots include public views -> recovery/
admission -> the same Radio's `WithFlashAccess` -> bound commit -> streaming/
native wrapper -> extracted SRAM helper -> ROM. No <=12 KiB claim is made.

The installed toolchain is TinyGo 0.42.0 / LLVM 22.1.4 with Go 1.27.3.
Runtime source hashes used for the allocator accounting:
`gc_blocks.go` SHA256
`8a8a37e21c7ef987ad25e6810c0857a62cfe41748aff85deec0dd3281217b49c`;
`gc_conservative.go` SHA256
`60e63df2bc0d60c9962dc60a918d243107a9827a7acd3c330ed7746fdbde3341`.
Host allocation deltas are Set/compaction 0, Get 1 exact output, and one-key
Keys 2 outputs. They are useful evidence, not native allocation/stack proof.

The hard-max NOR case reached a 69,640-byte snapshot and 16,488-byte cold payload.
After that cold commit: epoch 2/generation 65, slot 1, tail 90,352, 24 sector
erases, 69,640 copied bytes. The next 64 full-size small writes added exactly
208 model program calls and 6,144 program bytes, **zero erases/copies**;
generation 129, slot 65, tail 94,448 leaves 3,852 arena bytes and 63 slots.
Native radio pause/endurance metrics remain physical NOT_RUN.

Do not claim the 12 KiB peak from `unsafe.Sizeof`, global SRAM, heap address
space or a small consumer image. Count native/closure allocations, allocator
rounding and synchronous Go/C/ROM call chains; report any unresolved bound.
Real source/clock entropy qualification, power cuts, endurance, cache-off
execution, radio/Thread pause tolerance and complete application peak memory
remain unobserved. Larger banks increase worst-case erase pause. The pinned
ROM stuck-WIP path remains unbounded fail-stop/manual reset. CRC supplies no
whole-image antirollback, at-rest confidentiality or cryptographic safety claim.
