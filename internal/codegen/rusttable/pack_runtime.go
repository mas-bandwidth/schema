package rusttable

const packRuntime = `struct TablePack<'a> {
    base: *mut u8,
    at: usize,
    numbering: Option<&'a TableNumbering<'a>>,
    directory: &'a [TableNodeDirEntry],
}
impl TablePack<'_> {
    fn copying(&self) -> bool {
        !self.base.is_null()
    }
    fn allocate(&mut self, bytes: usize, align: usize) -> Result<usize, TableRefuseReason> {
        let at = table_align(self.at, align).ok_or(TableRefuseReason::BadLayout)?;
        self.at = at.checked_add(bytes).ok_or(TableRefuseReason::BadLayout)?;
        Ok(at)
    }
}
unsafe fn table_pack_record(
    context: TableContext,
    source: *const u8,
    destination: *mut u8,
    info: &TableTypeInfo,
    pack: &mut TablePack,
    active: bool,
) -> Result<(), TableRefuseReason> {
    unsafe {
        for field in info.fields {
            let visible = (field.guard.is_empty()
                || table_json_guard_holds(source, info, field.guard))
                && (!field.optional || source.add(field.present_offset as usize).read() != 0);
            if !visible {
                if pack.copying() {
                    (field.reset)(destination);
                }
                continue;
            }
            table_pack_field(context, source, destination, field, pack, active)?;
        }
        Ok(())
    }
}
unsafe fn table_pack_field(
    context: TableContext,
    source: *const u8,
    destination: *mut u8,
    f: &TableFieldInfo,
    pack: &mut TablePack,
    active: bool,
) -> Result<(), TableRefuseReason> {
    unsafe {
        let slot = source.add(f.offset as usize);
        let target = if pack.copying() {
            destination.add(f.offset as usize)
        } else {
            core::ptr::null_mut()
        };
        if let Some(sequence) = f.sequence {
            let info = sequence();
            let count = (slot.add(8) as *const i32).read();
            if count < 0 || (!active && count != 0) {
                return Err(TableRefuseReason::BadLayout);
            }
            let elements = info
                .cursor(context, slot)
                .ok_or(TableRefuseReason::BadLayout)?;
            if count == 0 {
                if pack.copying() {
                    (target as *mut i64).write(0);
                }
                return Ok(());
            }
            let size = (count as usize)
                .checked_mul(info.size)
                .ok_or(TableRefuseReason::BadLayout)?;
            let offset = pack.allocate(size, info.align)?;
            if pack.copying() {
                (target as *mut i64).write(pack.base.add(offset) as i64 - target as i64);
            }
            for (i, element) in elements.enumerate() {
                let dest = if pack.copying() {
                    let p = pack.base.add(offset + i * info.size);
                    core::ptr::copy_nonoverlapping(element, p, info.size);
                    p
                } else {
                    core::ptr::null_mut()
                };
                table_pack_scalar(context, element, dest, f, pack, active)?;
            }
            return Ok(());
        }
        if f.is_array {
            let count = if f.counted {
                (source.add(f.count_offset as usize) as *const i32).read()
            } else {
                f.array_bound
            };
            if count < 0 || count > f.array_bound {
                return Err(TableRefuseReason::BadLayout);
            }
            for i in 0..f.array_bound as usize {
                let live = active
                    && i < count as usize
                    && (!table_json_is_keyed(f) || table_json_keyed_slot_valid(f, i as i64));
                let dest = if pack.copying() {
                    target.add(i * f.elem_size as usize)
                } else {
                    core::ptr::null_mut()
                };
                table_pack_scalar(
                    context,
                    slot.add(i * f.elem_size as usize),
                    dest,
                    f,
                    pack,
                    live,
                )?;
            }
            return Ok(());
        }
        table_pack_scalar(context, slot, target, f, pack, active)
    }
}
unsafe fn table_pack_scalar(
    context: TableContext,
    source: *const u8,
    destination: *mut u8,
    f: &TableFieldInfo,
    pack: &mut TablePack,
    active: bool,
) -> Result<(), TableRefuseReason> {
    unsafe {
        if f.kind == 17 {
            let value = (source as *const i64).read();
            if !active && value != 0 {
                return Err(TableRefuseReason::BadLayout);
            }
            if pack.copying() {
                let info = f.table.ok_or(TableRefuseReason::BadLayout)?();
                let index = pack
                    .numbering
                    .unwrap()
                    .index(source as *const i64, info)
                    .ok_or(TableRefuseReason::BadLayout)?;
                let delta = if index == 0 {
                    0
                } else {
                    pack.base
                        .add(pack.directory[index as usize - 1].offset as usize)
                        as i64
                        - destination as i64
                };
                (destination as *mut i64).write(delta);
            }
            return Ok(());
        }
        if let Some(union) = f.arms {
            let union = union();
            let tag = (union.read_tag)(source);
            let arm = union
                .arms
                .get(tag as usize)
                .ok_or(TableRefuseReason::BadLayout)?;
            if let Some(field) = arm.field {
                let payload = (union.payload)(source, tag);
                if payload.is_null() {
                    return Err(TableRefuseReason::BadLayout);
                }
                let dest = if pack.copying() {
                    destination.add(payload.offset_from(source) as usize)
                } else {
                    core::ptr::null_mut()
                };
                table_pack_field(context, payload, dest, field, pack, active)?;
            }
            return Ok(());
        }
        if f.kind == 13 {
            table_pack_record(
                context,
                source,
                destination,
                f.table.ok_or(TableRefuseReason::BadLayout)?(),
                pack,
                active,
            )?;
        }
        Ok(())
    }
}
`
