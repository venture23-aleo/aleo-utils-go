use alloc::vec::Vec;
use core::{mem, ptr};

// --- Legacy helpers kept for output (hash/format) which still pack len|ptr ---
pub fn forget_buf_ptr(mut buf: Vec<u8>) -> *const u8 {
    // Guarantee capacity == length to make later deallocation using length safe.
    buf.shrink_to_fit();
    debug_assert_eq!(buf.capacity(), buf.len());
    let ptr = buf.as_ptr();
    mem::forget(buf);
    ptr
}

pub fn forget_buf_ptr_len(mut buf: Vec<u8>) -> u64 {
    buf.shrink_to_fit();
    debug_assert_eq!(buf.capacity(), buf.len());
    let len = buf.len() as u64;
    // Allocate a new vector with header + data so dealloc (which expects a header) works uniformly.
    // We intentionally do not reuse the original buffer to guarantee a header exists.
    let mut v: Vec<u8> = Vec::with_capacity(len as usize + 8);
    let cap = v.capacity();
    let base = v.as_mut_ptr();
    unsafe {
        // Write capacity header
        ptr::write_unaligned(base.cast::<u64>(), cap as u64);
        // Copy data bytes after header
        ptr::copy_nonoverlapping(buf.as_ptr(), base.add(8), len as usize);
        mem::forget(buf);
        let data_ptr = base.add(8) as *const u8 as usize as u64;
        mem::forget(v);
        (len << 32) | data_ptr
    }
}

// Header-based allocation (8-byte little-endian capacity header preceding data region)
// Returns a pointer to usable data (after the header). The second parameter passed from Go
// to `dealloc` is ignored for safety; capacity is always read from the header.
#[no_mangle]
pub extern "C" fn alloc(size: usize) -> *const u8 {
    // Allocate vector with space for header + requested size (length left 0; caller writes bytes).
    let mut v: Vec<u8> = Vec::with_capacity(size + 8);
    let full_cap = v.capacity();
    let base = v.as_mut_ptr(); // pointer to header start
    unsafe {
        // Store full vector capacity (not just requested size) so we can reconstruct exactly.
        ptr::write_unaligned(base.cast::<u64>(), full_cap as u64);
        // We purposely leave length at 0; caller will write directly into linear memory.
        let data_ptr = base.add(8);
        mem::forget(v);
        data_ptr as *const u8
    }
}

#[no_mangle]
pub extern "C" fn dealloc(data_ptr: *const u8, _ignored: usize) {
    if data_ptr.is_null() {
        return;
    }
    unsafe {
        let header_ptr = data_ptr.sub(8);
        let full_cap = ptr::read_unaligned(header_ptr.cast::<u64>()) as usize;
        // Rebuild Vec using the original full capacity (length 0 since caller relinquishes ownership)
        let _ = Vec::from_raw_parts(header_ptr.cast_mut(), 0, full_cap);
    }
}
