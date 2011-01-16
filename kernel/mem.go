package main

import "unsafe"

type Phys uint64

/*
	the whole address space is 256 TB
	a page directory pointer table adresses 512 GB
	a page directory addresses 1 GB
	a page table addresses 2 MB
	a page addresses 4 KB
*/

const (
	COREMAPSIZE   = 512
	max32         = 0xFFFFFFFF
	max64         = 0xFFFFFFFFFFFFFFFF
	kernelstart   uintptr = 0x100000
	E820FREE      uint64  = 1
	E820RSVD      uint64  = 2
	MAXE820       = 100   // maximum number of E820 entries
	PAGESIZE      = 4096
	TMPPAGES      uintptr = 0x40000000 // if you change this number you also need to change initpaging()
	STACK uintptr = 0x80000 // this number also appears in runtime/rt0.s and has to be page-aligned
	// STACK is also the location of the TLS
	PAGETABLESIZE = 512
	NUMTMPPAGES   = 256
	PAGEAVAIL     uint64 = 1
	PAGEWRITE     uint64 = 2
	GDTCODE       = ((1 << 11) | (1 << 12)) << 32
	GDTPRESENT    = (1 << 15) << 32
	GDTLONGMODE   = (1 << 21) << 32
	GDTDATA       = (1 << 12) << 32
	GDTWRITABLE   = (1 << 9) << 32
	ANTIPAGE      = max32 ^ (PAGESIZE - 1)
)

type coreMapEntry struct {
	addr uint64
	size uint64
}

type tempPage struct {
	addr Phys
	ref  int
}

var (
	e820map [][3]uint64
	e820num int
	coremap [COREMAPSIZE]coreMapEntry
	pml4    []uint64
	tmppage [NUMTMPPAGES]tempPage
	tmppt   []uint64
	gdt     []uint64
	heap    []uint32
	curstack *uint64
)

func invlpg(uintptr)
func setcr3([]uint64)
func lgdt([]uint64)
func loadsegs()
func memzero(uintptr, uint64)
func memcpy(dst uintptr, src uintptr, n uint64)

func pageroundup(n uint64) uint64 {
	return (n + PAGESIZE - 1) & (max64 ^ (PAGESIZE - 1))
}

func processe820() {
	var e820limits [2 * MAXE820]uint64
	k := 0
	for i := 0; i < e820num; i++ {
		e820limits[k] = pageroundup(e820map[i][0])
		e820limits[k+1] = pageroundup(e820map[i][0] + e820map[i][1])
		k += 2
	}
	swapped := true
	for swapped {
		swapped = false
		for i := 1; i < k; i++ {
			if e820limits[i] == e820limits[i-1] {
				e820limits[i], e820limits[k-1] = e820limits[k-1], e820limits[i]
				swapped = true
				k--
			}
			if e820limits[i] < e820limits[i-1] {
				e820limits[i], e820limits[i-1] = e820limits[i-1], e820limits[i]
				swapped = true
			}
		}
	}
	m := 0
	size := uint64(0)
	for i := 0; i < k-1; i++ {
		l := e820limits[i]
		found := false
		for j := 0; j < e820num; j++ {
			if l >= e820map[j][0] && l < e820map[j][0]+e820map[j][1] {
				if e820map[j][2] != E820FREE {
					goto cont
				} else {
					found = true
				}
			}
		}
		if found {
			coremap[m] = coreMapEntry{addr: l, size: e820limits[i+1] - l}
			size += e820limits[i+1] - l
			m++
		}
	cont:
	}
	if size < 16777216 {
		fuck("Sorry, GOFY doesn't run on toasters")
	}
	putnum((size+524288)/1048576, 10)
	puts(" MB memory\n")
}

func initframes() {
	e820num = int(*(*uint32)(unsafe.Pointer(uintptr(0x600))))
	if e820num == 0 {
		fuck("E820 fucked up")
	}
	if e820num > MAXE820 {
		fuck("E820 map too large")
	}

	mh := (*SliceHeader)(unsafe.Pointer(&e820map))
	mh.Data = 0x608
	mh.Len = e820num + 3
	mh.Cap = mh.Len
	e820map[e820num] = [3]uint64{0, 0x1000, E820RSVD}
	highest := *(*uint64)(unsafe.Pointer(uintptr(0x502)))
	highest = pageroundup(highest)
	e820map[e820num+1] = [3]uint64{uint64(kernelstart), highest - uint64(kernelstart), E820RSVD}
	e820map[e820num+2] = [3]uint64{uint64(STACK) - PAGESIZE, 2*PAGESIZE, E820RSVD}
	e820num += 3
	processe820()

	heaph := (*SliceHeader)(unsafe.Pointer(&heap))
	heaph.Data = (uintptr(highest) + 0xFFFFF) & (max64 ^ 0xFFFFF)
	heaph.Len = 0
	heaph.Cap = 0
}

func initpaging() {
	var i uintptr

	gdth := (*SliceHeader)(unsafe.Pointer(&gdt))
	gdth.Data = uintptr(falloc(1)) // huge waste of space, but who cares?
	gdth.Len = 3
	gdth.Cap = 512

	gdt[0] = 0
	gdt[1] = GDTCODE | GDTLONGMODE | GDTPRESENT
	gdt[2] = GDTDATA | GDTWRITABLE | GDTPRESENT
	lgdt(gdt)
	loadsegs()

	pml4h := (*SliceHeader)(unsafe.Pointer(&pml4))
	pml4h.Data = uintptr(falloc(1))
	pml4h.Len = 512
	pml4h.Cap = 512

	pdp0 := falloc(1)
	pd0 := falloc(1)
	pd1 := falloc(1)
	pt0 := falloc(1)

	tmppth := (*SliceHeader)(unsafe.Pointer(&tmppt))
	tmppth.Data = uintptr(falloc(1))
	tmppth.Len = 512
	tmppth.Cap = 512

	pml4[0] = uint64(pdp0) | PAGEAVAIL | PAGEWRITE
	*(*uint64)(unsafe.Pointer(uintptr(pdp0))) = uint64(pd0) | PAGEAVAIL | PAGEWRITE
	*(*uint64)(unsafe.Pointer(uintptr(pdp0 + 8))) = uint64(pd1) | PAGEAVAIL | PAGEWRITE
	*(*uint64)(unsafe.Pointer(uintptr(pd0))) = uint64(pt0) | PAGEAVAIL | PAGEWRITE
	*(*uint64)(unsafe.Pointer(uintptr(pd1))) = uint64(tmppth.Data) | PAGEAVAIL | PAGEWRITE
	for i = 0; i < 512; i++ {
		*(*uint64)(unsafe.Pointer(uintptr(pt0) + 8*i)) = (uint64(PAGESIZE * i)) | PAGEAVAIL | PAGEWRITE
	}
	curstack = (*uint64)(unsafe.Pointer(uintptr(pt0) + 8*((STACK-1)/PAGESIZE)))
	setcr3(pml4)
}

func falloc(n uint64) (p Phys) {
	if n == 0 {
		return 0
	}
	for i := 0; coremap[i].size != 0; i++ {
		if coremap[i].size >= PAGESIZE*n {
			p = Phys(coremap[i].addr)
			coremap[i].addr += PAGESIZE * n
			coremap[i].size -= PAGESIZE * n
			if coremap[i].size == 0 {
				i++
				for ; coremap[i].size != 0; i++ {
					coremap[i-1] = coremap[i]
				}
			}
			return
		}
	}
	fuck("out of memory")
	return 0
}

func ffree(p Phys, n uint64) {
	if n == 0 {
		return
	}
	for i := 0; coremap[i].size != 0; i++ {
		if i > 0 && coremap[i-1].addr+coremap[i-1].size == uint64(p) {
			coremap[i-1].size += n
			if uint64(p)+n == coremap[i].addr {
				coremap[i-1].size += coremap[i].size
				i++
				for ; coremap[i].size != 0; i++ {
					coremap[i-1] = coremap[i]
				}
			}
		} else {
			if uint64(p)+n == coremap[i].addr {
				coremap[i].addr -= n
				coremap[i].size += n
			} else {
				t := coreMapEntry{addr: uint64(p), size: n}
				for ; t.size != 0; i++ {
					t, coremap[i] = coremap[i], t
				}
				coremap[i].size = 0
			}
		}
	}
}

func tmpmap(p Phys) (v uintptr) {
	if p&(PAGESIZE-1) != 0 {
		fuck("tmpmap called with non-aligned address")
	}
	for k := range tmppage {
		if tmppage[k].addr == p {
			tmppage[k].ref++
			return TMPPAGES + PAGESIZE*uintptr(k)
		}
	}
	for k := range tmppage {
		if tmppage[k].ref == 0 {
			tmppage[k].addr = p
			tmppage[k].ref++
			tmppt[k] = uint64(p) | PAGEAVAIL | PAGEWRITE
			v = TMPPAGES + PAGESIZE*uintptr(k)
			invlpg(v)
			return
		}
	}
	fuck("no free tmp pages")
	return 0
}

func tmpfree(v uintptr) {
	if v < TMPPAGES || v >= TMPPAGES+NUMTMPPAGES*PAGESIZE {
		fuck("tmpfree called with invalid address")
	}
	v = (v - TMPPAGES) / PAGESIZE
	if tmppage[v].ref > 0 {
		tmppage[v].ref--
	}
}

func decode(v uintptr) (pdp, pd, pt, page, disp int) {
	pdp = int((v >> 39) & 0x1FF)
	pd = int((v >> 30) & 0x1FF)
	pt = int((v >> 21) & 0x1FF)
	page = int((v >> 12) & 0x1FF)
	disp = int(v & (PAGESIZE - 1))
	return
}

func mappage(v uintptr, p Phys, flags uint64) {
	var pdp, pd, pt *[512]uint64
	var zero bool

	npdp, npd, npt, npage, disp := decode(v)
	if p&(PAGESIZE-1) != 0 || disp != 0 {
		fuck("map called with invalid address")
	}

	zero = false
	pdpa := Phys(pml4[npdp])
	if pdpa&1 == 0 {
		pdpa = falloc(1)
		pml4[npdp] = uint64(pdpa) | PAGEAVAIL | PAGEWRITE
		zero = true
	}
	pdp = (*[512]uint64)(unsafe.Pointer(tmpmap(pdpa & ANTIPAGE)))
	if zero {
		for k := range pdp {
			pdp[k] = 0
		}
	}

	zero = false
	pda := Phys(pdp[npd])
	if pda&1 == 0 {
		pda = falloc(1)
		pdp[npd] = uint64(pda) | PAGEAVAIL | PAGEWRITE
		zero = true
	}
	pd = (*[512]uint64)(unsafe.Pointer(tmpmap(pda & ANTIPAGE)))
	if zero {
		for k := range pd {
			pd[k] = 0
		}
	}

	zero = false
	pta := Phys(pd[npt])
	if pta&1 == 0 {
		pta = falloc(1)
		pd[npt] = uint64(pta) | PAGEAVAIL | PAGEWRITE
		zero = true
	}
	pt = (*[512]uint64)(unsafe.Pointer(tmpmap(pta & ANTIPAGE)))
	if zero {
		for k := range pt {
			pt[k] = 0
		}
	}

	pt[npage] = uint64(p) | flags
	invlpg(v)
	tmpfree(uintptr(unsafe.Pointer(pdp)))
	tmpfree(uintptr(unsafe.Pointer(pd)))
	tmpfree(uintptr(unsafe.Pointer(pt)))
	return
}

func printheap() {
	heaph := (*SliceHeader)(unsafe.Pointer(&heap))
	for i := 0; i < len(heap); {
		len := heap[i]
		if len&1 != 0 {
			putc('X')
		} else {
			putc('O')
		}
		putc(' ')
		putnum(uint64(heaph.Data)+uint64(i+1)*4, 16)
		putc(' ')
		putnum(uint64(len&(max32^7)), 16)
		if heap[i] != heap[i+int(len)/4+1] {
			puts(" !!! CORRUPTED ")
			putnum(uint64(heap[i]), 16)
			puts(" != ")
			putnum(uint64(heap[i+int(len)/4+1]), 16)
		}
		putc(10)
		i += int(2 + len/4)
	}
}

/*
	heap format:
	len (dword), data, len (dword)
	the second len is used both for consistency checking and walking backwards
	objects larger than 4 GB shouldn't be placed on the heap
	the length is qword-aligned
	this function is guaranteed to suck
*/

func kmalloc(size uintptr) uintptr {
	if size == 0 {
		return 0
	}
	if size+7 > max32 {
		fuck("kmalloc called with too large argument (>4GB)")
	}
	heaph := (*SliceHeader)(unsafe.Pointer(&heap))
	N := uint32((size + 7) & (max64 ^ 7))
	for i := 0; i < len(heap); {
		len := heap[i]
		if len&1 == 0 && len >= N {
			if len <= N+8 {
				heap[i] |= 1
				heap[i+int(len)/4+1] |= 1
			} else {
				heap[i] = N | 1
				heap[i+int(N)/4+1] = N | 1
				heap[i+int(N)/4+2] = len - N - 8
				heap[i+int(len)/4+1] = len - N - 8
			}
			return heaph.Data + uintptr(i+1)*4
		}
		i += int(2 + len/4)
	}
	npages := (N + 8 + (PAGESIZE - 1)) / PAGESIZE
	nf := falloc(uint64(npages))
	for i := uintptr(0); i < uintptr(npages); i++ {
// the region just below the stack is unmapped to prevent weird things from happening if the stack grows too large
		if i != (STACK - 1 - PAGESIZE) / PAGESIZE {
			mappage(uintptr(heaph.Data)+uintptr(heaph.Len)*4+i*PAGESIZE, nf+Phys(i)*PAGESIZE, PAGEAVAIL|PAGEWRITE)
		} else {
			mappage(uintptr(heaph.Data)+uintptr(heaph.Len)*4+i*PAGESIZE, 0, 0)
		}
	}
	i := uint32(heaph.Len)
	heaph.Len += int(npages * PAGESIZE / 4)
	heaph.Cap = heaph.Len
	heap[i] = N | 1
	heap[i+(N/4)+1] = N | 1
	heap[i+(N/4)+2] = npages*PAGESIZE - N - 16
	heap[i+npages*1024-1] = heap[i+(N/4)+2]
	return heaph.Data + uintptr(i+1)*4
}

func kfree(p uintptr) {
	heaph := (*SliceHeader)(unsafe.Pointer(&heap))
	if p < heaph.Data && p >= heaph.Data+uintptr(heaph.Len)*4 || p&3 != 0 {
		fuck("kfree called with invalid pointer")
	}
	i := uint32((p-heaph.Data)/4 - 1)
	len := heap[i]
	if len&1 == 0 {
		fuck("double kfree")
	}
	if len != heap[i+len/4+1] {
		fuck("kernel heap corruption")
	}
	heap[i] &= max32 ^ 1
	heap[i+len/4+1] &= max32 ^ 1
	if i > 0 {
		prev := i - heap[i-1]/4 - 2
		if heap[prev]&1 == 0 {
			heap[prev] += heap[i] + 8
			heap[i+len/4+1] = heap[prev]
			i = prev
		}
	}
	next := i + heap[i]/4 + 2
	if next < uint32(heaph.Len) {
		if heap[next]&1 == 0 {
			heap[i] += heap[next] + 8
			heap[next+heap[next]/4+1] = heap[i]
		}
	}
}
