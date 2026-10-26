" For TestRealFunctions: the functions for mappings, aliases and params from
" Vim script, and contextFiletype.
inoremap <expr> <C-x>1 goda_cmp#map#insert_item(0)
inoremap <expr> <C-x>2 goda_cmp#map#complete_common_string()
function! DumpCurrent() abort
  let cur = goda_cmp#get_current()
  call writefile([json_encode({'current': {
        \ 'mark': get(get(cur.sourceOptions, 'around', {}), 'mark', ''),
        \ 'visible': goda_cmp#visible()}})], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-x>3 <Cmd>call DumpCurrent()<CR>
" around2: around with params of its own, its duplicates kept.
call goda_cmp#alias('source', 'around2', 'around')
call goda_cmp#patch_global({
      \ 'sources': ['around', 'around2'],
      \ 'sourceParams': {'around2': {'minLength': 9}},
      \ 'sourceOptions': {'around2': {'dup': 'keep'}},
      \ 'contextFiletype': 'treesitter',
      \ })
call goda_cmp#patch_filetype('lua', {'sourceOptions': {'around': {'mark': 'LUA'}}})
