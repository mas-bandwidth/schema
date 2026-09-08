package ctable

// Pointer JSON shares the descriptor walk with the fixed class. The map is
// per operation: text labels on read, reference counts and labels on write.
const tableJsonGraphSource = `
/* ---- json graph walk: begin ---- */
typedef struct TableJsonGraphEntry
{
    uint64_t key;
    int64_t count, label;
    int open;
    uint32_t node;
    const TableTypeInfo * type;
} TableJsonGraphEntry;

typedef struct TableJsonGraphMap
{
    TableJsonGraphEntry * entries;
    uint64_t capacity, count;
    TableAllocator allocator;
} TableJsonGraphMap;

static SCHEMA_UNUSED uint64_t table_json_graph_slot( const TableJsonGraphMap * map, uint64_t key )
{
    uint64_t hash = key * UINT64_C(0x9E3779B97F4A7C15);
    uint64_t at;
    hash ^= hash >> 29; at = hash & (map->capacity-1);
    while ( map->entries[at].key != 0 && map->entries[at].key != key ) { at = (at+1) & (map->capacity-1); }
    return at;
}

static SCHEMA_UNUSED TableJsonGraphEntry * table_json_graph_find( TableJsonGraphMap * map, uint64_t key )
{
    TableJsonGraphEntry * entry;
    if ( map->capacity == 0 ) { return NULL; }
    entry = &map->entries[table_json_graph_slot(map,key)];
    return entry->key == key ? entry : NULL;
}

static SCHEMA_UNUSED TableJsonGraphEntry * table_json_graph_reach( TableJsonGraphMap * map, uint64_t key, int * taken )
{
    TableJsonGraphEntry * entry;
    if ( map->capacity == 0 || map->count >= map->capacity - map->capacity/4 - 1 )
    {
        TableJsonGraphMap grown;
        uint64_t i;
        grown.capacity = map->capacity ? map->capacity*4 : 64;
        if ( grown.capacity < map->capacity || grown.capacity > INT64_MAX / sizeof(TableJsonGraphEntry) ) { return NULL; }
        grown.count = map->count; grown.allocator=map->allocator;
        grown.entries = (TableJsonGraphEntry *)table_allocate(map->allocator,(int64_t)grown.capacity*sizeof(TableJsonGraphEntry));
        if ( grown.entries == NULL ) { return NULL; }
        for ( i = 0; i < map->capacity; i++ )
        {
            if ( map->entries[i].key ) { grown.entries[table_json_graph_slot(&grown,map->entries[i].key)] = map->entries[i]; }
        }
        table_release(map->allocator,map->entries); *map = grown;
    }
    entry = &map->entries[table_json_graph_slot(map,key)];
    *taken = entry->key != key;
    if ( *taken ) { entry->key = key; map->count++; }
    return entry;
}

typedef struct TableJsonGraphIn
{
    TableWorker * worker;
    TableJsonGraphMap labels;
} TableJsonGraphIn;

static SCHEMA_UNUSED int table_json_scan_label( TableJsonIn * in, uint64_t * label )
{
    uint64_t value = 0;
    table_json_space(in);
    if ( in->pos >= in->size || in->text[in->pos] < '1' || in->text[in->pos] > '9' ) { in->bad = 1; return 0; }
    while ( in->pos < in->size && in->text[in->pos] >= '0' && in->text[in->pos] <= '9' )
    {
        uint64_t digit = (uint64_t)(in->text[in->pos]-'0');
        if ( value > (UINT64_MAX-digit)/10 ) { in->bad = 1; return 0; }
        value = value*10+digit; in->pos++;
    }
    *label = value; return 1;
}

static SCHEMA_UNUSED void * table_json_emplace( TableJsonGraphIn * graph, void * slot, const TableTypeInfo * type )
{
    uint32_t at = table_worker_bump(graph->worker,type->size);
    void * node;
    if ( at == kTableAllocFailed ) { return NULL; }
    node = table_arena_at(graph->worker->arena,at);
    type->reset(node); ((TableRef *)slot)->value = at; return node;
}

static SCHEMA_UNUSED int table_json_read_blob( TableJsonIn * in, void * slot, const TableFieldInfo * f )
{
    TableJsonGraphIn * graph=(TableJsonGraphIn *)in->graph;
    TableRef * ref=(TableRef *)slot;
    int64_t mark, symbols=0, length, placed=0, at;
    int malformed=0, held=0;
    uint32_t accumulator=0;
    uint8_t * data;
    if(graph==NULL || table_json_peek(in)!='"') { in->bad=1; return 0; }
    ref->value=0;
    if(strcmp(f->type_name,"string")==0)
    {
        int32_t count=0, written=0;
        char * text;
        mark=in->pos;
        if(!table_json_scan_string(in,NULL,0,&count)) { return 0; }
        in->pos=mark; text=table_string_emplace(graph->worker,ref,NULL,count);
        if(text==NULL) { in->bad=1; return 0; }
        return table_json_scan_string(in,text,count,&written);
    }
    mark=++in->pos;
    for(;;)
    {
        char c;
        if(in->pos>=in->size) { in->bad=1; return 0; }
        c=in->text[in->pos++];
        if(c=='"') { break; }
        if(c=='=' || malformed) { continue; }
        if(table_json_base64_value((uint8_t)c)<0) { malformed=1; continue; }
        symbols++;
    }
    if(malformed) { in->report->kind_mismatch++; return 1; }
    length=symbols*6/8;
    data=table_bytes_emplace(graph->worker,ref,length);
    if(data==NULL) { in->bad=1; return 0; }
    for(at=mark; in->text[at]!='"'; at++)
    {
        int32_t symbol=table_json_base64_value((uint8_t)in->text[at]);
        if(symbol<0) { continue; }
        accumulator=(accumulator<<6)|(uint32_t)symbol; held+=6;
        if(held>=8) { held-=8; if(placed<length) { data[placed++]=(uint8_t)((accumulator>>held)&0xff); } }
    }
    return 1;
}

static SCHEMA_UNUSED int table_json_read_pointer( TableJsonIn * in, void * slot, const TableFieldInfo * f, int32_t depth )
{
    TableJsonGraphIn * graph = (TableJsonGraphIn *)in->graph;
    TableJsonGraphEntry * entry;
    char key[kTableJsonMaxKey], c;
    int32_t length = 0;
    uint64_t label = 0;
    int taken = 0, bare;
    void * node;
    if(graph!=NULL && f->table==NULL) { return table_json_read_blob(in,slot,f); }
    if ( graph == NULL || depth+1 > kTableJsonMaxDepth || table_json_peek(in) != '{' ) { in->bad = 1; return 0; }
    in->pos++; c = table_json_peek(in);
    if ( c == '}' )
    {
        in->pos++;
        if ( table_json_emplace(graph,slot,f->table) == NULL ) { in->bad = 1; return 0; }
        return 1;
    }
    if ( !table_json_scan_string(in,key,kTableJsonMaxKey-1,&length) ) { return 0; }
    key[length] = 0;
    if ( table_json_peek(in) != ':' ) { in->bad = 1; return 0; }
    in->pos++;
    if ( strcmp(key,"&node") != 0 )
    {
        node = table_json_emplace(graph,slot,f->table);
        if ( node == NULL ) { in->bad = 1; return 0; }
        return table_json_read_table_keys(in,node,f->table,depth+1,key);
    }
    if ( !table_json_scan_label(in,&label) ) { return 0; }
    entry = table_json_graph_reach(&graph->labels,label,&taken);
    if ( entry == NULL ) { in->bad = 1; return 0; }
    c = table_json_peek(in);
    if ( c == ',' ) { in->pos++; c = table_json_peek(in); }
    bare = c == '}';
    if ( bare == taken ) { in->bad = 1; return 0; }
    if ( bare )
    {
        in->pos++;
        if ( entry->open ) { in->bad = 1; return 0; }
        ((TableRef *)slot)->value = 0;
        if ( entry->type == NULL ) { return 1; }
        if ( entry->type != f->table ) { in->report->kind_mismatch++; return 1; }
        ((TableRef *)slot)->value = entry->node; return 1;
    }
    node = table_json_emplace(graph,slot,f->table);
    if ( node == NULL ) { in->bad = 1; return 0; }
    entry->open = 1;
    if ( !table_json_read_table_keys(in,node,f->table,depth+1,NULL) ) { return 0; }
    entry = table_json_graph_find(&graph->labels,label);
    if ( entry == NULL ) { in->bad = 1; return 0; }
    entry->node = (uint32_t)((TableRef *)slot)->value; entry->type = f->table; entry->open = 0; return 1;
}

static SCHEMA_UNUSED int table_json_skipped_ampersand( TableJsonIn * in, const char * key )
{
    TableJsonGraphIn * graph = (TableJsonGraphIn *)in->graph;
    uint64_t label = 0;
    int taken = 0;
    if ( graph == NULL || strcmp(key,"&node") != 0 ) { in->bad = 1; return 0; }
    if ( !table_json_scan_label(in,&label) ) { return 0; }
    if ( table_json_graph_reach(&graph->labels,label,&taken) == NULL ) { in->bad = 1; return 0; }
    return 1;
}

typedef struct TableJsonGraphOut
{
    TableJsonGraphMap nodes;
    int counting;
    int64_t next_label;
} TableJsonGraphOut;

static SCHEMA_UNUSED int table_json_write_pointer( TableJsonOut * out, const void * slot, const TableFieldInfo * f, int32_t depth )
{
    TableJsonGraphOut * graph = (TableJsonGraphOut *)out->graph;
    const void * node = table_ref_at(NULL,(const TableRef *)slot);
    TableJsonGraphEntry * entry;
    int taken = 0, any = 1;
    int64_t before;
    if ( graph == NULL ) { return 0; }
    if ( node == NULL ) { table_json_raw(out,"null",4); return 1; }
    entry = table_json_graph_reach(&graph->nodes,(uint64_t)(uintptr_t)node,&taken);
    if ( entry == NULL ) { return 0; }
    if(f->table==NULL)
    {
        const TableBlob * blob=(const TableBlob *)node;
        if(graph->counting) { entry->count++; return 1; }
        if(entry->count>1 || blob->length>INT32_MAX) { return 0; }
        if(strcmp(f->type_name,"string")==0) { table_json_write_string(out,(const char *)(blob+1),(int32_t)blob->length); }
        else { table_json_write_base64(out,(const uint8_t *)(blob+1),(int32_t)blob->length); }
        return 1;
    }
    if ( graph->counting )
    {
        entry->count++;
        if ( !taken ) { return entry->open == 0; }
        entry->open = 1;
        if ( !table_json_write_value(out,node,f->table,depth) ) { return 0; }
        entry = table_json_graph_find(&graph->nodes,(uint64_t)(uintptr_t)node);
        if ( entry == NULL ) { return 0; }
        entry->open = 0; return 1;
    }
    if ( entry->count <= 1 ) { return table_json_write_value(out,node,f->table,depth); }
    if ( depth > kTableJsonMaxDepth ) { return 0; }
    table_json_put(out,'{'); table_json_line(out,depth+1); table_json_raw(out,"\"&node\": ",9);
    if ( entry->label )
    {
        table_json_write_unsigned(out,(uint64_t)entry->label);
        table_json_line(out,depth); table_json_put(out,'}'); return 1;
    }
    entry->label = ++graph->next_label;
    table_json_write_unsigned(out,(uint64_t)entry->label);
    before = out->offset;
    if ( !table_json_write_fields(out,node,f->table,depth,&any) || out->offset == before ) { return 0; }
    table_json_line(out,depth); table_json_put(out,'}'); return 1;
}

static SCHEMA_UNUSED int table_json_read_graph( TableWorker * worker, void * root, const TableTypeInfo * info, const char * text, int64_t bytes, TableReport * report )
{
    TableJsonGraphIn graph;
    TableJsonIn in;
    TableReport ignored = {0};
    int ok;
    memset(&graph,0,sizeof(graph)); graph.worker = worker; graph.labels.allocator=worker->arena!=NULL ? worker->arena->allocator : table_default_allocator();
    memset(&in,0,sizeof(in)); in.text = text; in.size = bytes; in.report = report ? report : &ignored; in.graph = &graph;
    if ( root == NULL || worker->arena == NULL || text == NULL || bytes < 0 ) { in.report->malformed = 1; return 0; }
    info->reset(root);
    ok = table_json_read_table(&in,root,info,0);
    if ( ok ) { table_json_space(&in); if ( in.pos != in.size ) { in.bad = 1; } }
    table_release(graph.labels.allocator,graph.labels.entries);
    if ( in.bad || !ok ) { in.report->malformed = 1; return 0; }
    return 1;
}

static SCHEMA_UNUSED int64_t table_json_write_graph( const void * root, const TableTypeInfo * info, char * buffer, int64_t capacity, TableAllocator allocator )
{
    TableJsonGraphOut graph;
    TableJsonOut count, out;
    TableJsonGraphEntry * entry;
    int taken = 0, ok;
    if ( root == NULL ) { return -1; }
    memset(&graph,0,sizeof(graph)); graph.counting = 1; graph.nodes.allocator=allocator;
    entry = table_json_graph_reach(&graph.nodes,(uint64_t)(uintptr_t)root,&taken);
    if ( entry == NULL ) { return -1; }
    entry->open = 1;
    memset(&count,0,sizeof(count)); count.graph = &graph;
    ok = table_json_write_value(&count,root,info,0);
    graph.counting = 0;
    memset(&out,0,sizeof(out)); out.buffer = buffer; out.capacity = capacity; out.graph = &graph;
    if ( ok ) { ok = table_json_write_value(&out,root,info,0); }
    table_release(graph.nodes.allocator,graph.nodes.entries);
    if ( !ok ) { return -1; }
    table_json_put(&out,'\n'); return out.overflow ? -1 : out.offset;
}
/* ---- json graph walk: end ---- */

#ifdef SCHEMA_TABLE_SEQUENCE_RUNTIME
static SCHEMA_UNUSED int table_json_write_list(TableJsonOut * out,const void * storage,const TableFieldInfo * f,int32_t depth)
{
    const TableSequence * list=(const TableSequence *)storage;
    TableSequenceCursor cursor=table_sequence_cursor(NULL,list,f->elem_size); int32_t i;
    if(!cursor.ok) { return 0; }
    table_json_put(out,'[');
    for(i=0;i<list->count;i++) {
        const void * element=table_sequence_next(&cursor); if(element==NULL) { return 0; }
        if(i) { table_json_put(out,','); } table_json_line(out,depth+1);
        if(!table_json_write_scalar(out,element,f,depth+1)) { return 0; }
    }
    if(list->count) { table_json_line(out,depth); } table_json_put(out,']'); return 1;
}
static SCHEMA_UNUSED int table_json_read_list(TableJsonIn * in,void * storage,const TableFieldInfo * f,int32_t depth)
{
    TableJsonGraphIn * graph=(TableJsonGraphIn *)in->graph;
    TableSequence * list=(TableSequence *)storage; char shape=table_json_element_shape(f);
    if(graph==NULL || table_json_peek(in)!='[') { in->bad=1; return 0; }
    memset(list,0,sizeof(*list)); in->pos++;
    for(;;) {
        char c=table_json_peek(in); void * element;
        if(c==']') { in->pos++; return 1; }
        if(c==0) { in->bad=1; return 0; }
        element=table_sequence_add(graph->worker,list,f->elem_size);
        if(element==NULL) { in->bad=1; return 0; }
        if(f->kind==13) { f->table->reset(element); }
        if(table_json_value_shape(in)!=shape && !(f->kind==17 && table_json_value_shape(in)=='z')) {
            in->report->kind_mismatch++; if(!table_json_skip_value(in,depth+1)) { return 0; }
        } else if(!table_json_read_scalar(in,element,f,depth+1)) { return 0; }
        c=table_json_peek(in);
        if(c==',') { in->pos++; continue; }
        if(c==']') { in->pos++; return 1; }
        in->bad=1; return 0;
    }
}
#endif

#ifdef SCHEMA_TABLE_MAP_RUNTIME
static SCHEMA_UNUSED TableJsonInteger table_json_interpret_exact( const char * token, int32_t length )
{
    TableJsonInteger out;
    out.magnitude = 0;
    out.negative = 0;
    out.fractional = 0;
    out.saturated = 0;
    out.finite = 1;
    int32_t i = 0;
    if ( i < length && token[i] == '-' ) { out.negative = 1; i++; } // WalkNumber refuses a leading plus
    const char * int_digits = token + i;
    int32_t int_len = 0;
    while ( i < length && token[i] >= '0' && token[i] <= '9' ) { int_len++; i++; }
    const char * frac_digits = token + i;
    int32_t frac_len = 0;
    if ( i < length && token[i] == '.' )
    {
        i++;
        frac_digits = token + i;
        while ( i < length && token[i] >= '0' && token[i] <= '9' ) { frac_len++; i++; }
    }
    int64_t exp = 0;
    if ( i < length && ( token[i] == 'e' || token[i] == 'E' ) )
    {
        i++;
        int exp_negative = 0;
        if ( i < length && ( token[i] == '-' || token[i] == '+' ) ) { exp_negative = token[i] == '-'; i++; }
        while ( i < length && token[i] >= '0' && token[i] <= '9' )
        {
            if ( exp < 100000 ) { exp = exp * 10 + ( token[i] - '0' ); }
            i++;
        }
        if ( exp_negative ) { exp = -exp; }
    }
    // leading and trailing zeros stripped, so the last digit kept is significant
    int32_t start = 0, end = int_len + frac_len;
    int64_t point = (int64_t) int_len + exp;
    while ( start < end && ( start < int_len ? int_digits[start] : frac_digits[start - int_len] ) == '0' ) { start++; point--; }
    while ( end > start && ( end - 1 < int_len ? int_digits[end - 1] : frac_digits[end - 1 - int_len] ) == '0' ) { end--; }
    const int64_t digits = end - start;
    if ( digits == 0 ) { out.negative = 0; return out; } // the value is zero, and -0 IS zero
    if ( point < digits ) { out.fractional = 1; return out; } // a significant digit below the point
    if ( point > 20 ) { out.magnitude = UINT64_MAX; out.saturated = 1; return out; }
    for ( int32_t k = start; k < end; k++ )
    {
        const uint64_t digit = (uint64_t) ( ( k < int_len ? int_digits[k] : frac_digits[k - int_len] ) - '0' );
        if ( out.magnitude > ( UINT64_MAX - digit ) / 10 ) { out.magnitude = UINT64_MAX; out.saturated = 1; return out; }
        out.magnitude = out.magnitude * 10 + digit;
    }
    for ( int64_t k = digits; k < point; k++ ) // the point's own zeros, which no digit spells
    {
        if ( out.magnitude > UINT64_MAX / 10 ) { out.magnitude = UINT64_MAX; out.saturated = 1; return out; }
        out.magnitude *= 10;
    }
    return out;
}

static SCHEMA_UNUSED int table_json_write_map(TableJsonOut * out,const void * storage,const TableFieldInfo * f,int32_t depth)
{
    const TableSequence * map=(const TableSequence *)storage;
    TableSequenceCursor cursor=table_sequence_cursor(NULL,map,f->elem_size); int32_t i;
    const TableFieldInfo * key=&f->table->fields[0]; const TableFieldInfo * value=&f->table->fields[1];
    if(!cursor.ok) { return 0; } table_json_put(out,'{');
    for(i=0;i<map->count;i++) {
        const uint8_t * entry=(const uint8_t *)table_sequence_next(&cursor); if(entry==NULL) { return 0; }
        if(i) { table_json_put(out,','); } table_json_line(out,depth+1);
        if(key->kind==12) { table_json_write_string(out,(const char *)entry+key->offset,table_json_count(entry,key)); }
        else {
            table_json_put(out,'"');
            if(key->kind>=2 && key->kind<=5) { table_json_write_signed(out,table_json_get_signed(entry+key->offset,key->elem_size)); }
            else { table_json_write_unsigned(out,table_json_get_raw(entry+key->offset,key->elem_size)); }
            table_json_put(out,'"');
        }
        table_json_raw(out,": ",2); if(!table_json_write_field(out,entry,value,depth+1)) { return 0; }
    }
    if(map->count) { table_json_line(out,depth); } table_json_put(out,'}'); return 1;
}
static SCHEMA_UNUSED int table_json_map_key_value(const char * token,int32_t length,const TableFieldInfo * key,int64_t * value,int * fits)
{
    TableReport scratch={0}; TableJsonIn probe; TableJsonInteger number; int integral=0,moved=0;
    memset(&probe,0,sizeof(probe)); probe.text=token; probe.size=length; probe.report=&scratch; *fits=0;
    table_json_space(&probe); if(probe.pos!=0 || !table_json_walk_number(&probe,&integral) || probe.pos!=length) { return 0; }
    number=table_json_interpret_exact(token,length); if(number.fractional || number.saturated) { return 1; }
    *value=table_json_integer_in_domain(number,key->kind>=2 && key->kind<=5,(int32_t)key->elem_size,&moved); *fits=!moved; return 1;
}
static SCHEMA_UNUSED int table_json_read_map(TableJsonIn * in,void * storage,const TableFieldInfo * f,int32_t depth)
{
    TableJsonGraphIn * graph=(TableJsonGraphIn *)in->graph; TableSequence * map=(TableSequence *)storage;
    const TableFieldInfo * key=&f->table->fields[0]; const TableFieldInfo * value=&f->table->fields[1]; char shape=table_json_shape(value);
    if(graph==NULL || table_json_peek(in)!='{' || depth+1>kTableJsonMaxDepth) { in->bad=1; return 0; }
    memset(map,0,sizeof(*map)); in->pos++;
    for(;;) {
        char c=table_json_peek(in),token[kTableJsonMaxKey]; int32_t length=0,before; int64_t number=0; int place=1; void * entry;
        TableReport scratch={0}; TableJsonIn scan=*in; scan.report=&scratch;
        if(c=='}') { in->pos++; return 1; } if(c==0) { in->bad=1; return 0; }
        if(!table_json_scan_string(&scan,token,kTableJsonMaxKey-1,&length)) { in->pos=scan.pos; in->bad=1; return 0; }
        in->pos=scan.pos; token[length]=0;
        if(table_json_peek(in)!=':') { in->bad=1; return 0; } in->pos++;
        if(key->kind==12) {
            if(scratch.clamped || length>key->array_bound) { in->report->clamped++; place=0; }
        } else {
            int fits=0;
            if(scratch.clamped) { in->report->kind_mismatch++; place=0; }
            else if(!table_json_map_key_value(token,length,key,&number,&fits)) { in->bad=1; return 0; }
            else if(!fits) { in->report->kind_mismatch++; place=0; }
        }
        before=map->count; entry=place ? f->place(graph->worker,map,token,length,number) : NULL;
        if(place && entry==NULL) { in->bad=1; return 0; }
        if(entry!=NULL && map->count==before) { in->report->duplicate++; }
        c=table_json_value_shape(in);
        if(entry==NULL) { if(!table_json_skip_value(in,depth+1)) { return 0; } }
        else if(c!=shape && !(value->kind==17 && !value->is_array && c=='z')) {
            in->report->kind_mismatch++; if(!table_json_skip_value(in,depth+1)) { return 0; }
        } else if(!table_json_read_field(in,entry,value,depth+1)) { return 0; }
        c=table_json_peek(in); if(c==',') { in->pos++; continue; } if(c=='}') { in->pos++; return 1; }
        in->bad=1; return 0;
    }
}
#endif
`
