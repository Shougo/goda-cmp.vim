" For TestRealFunctions: the functions for mappings, params of an alias from
" Vim script, and contextFiletype.
inoremap <expr> <C-x>1 goda_cmp#map#insert_item(0)
inoremap <expr> <C-x>2 goda_cmp#map#complete_common_string()
function! DumpCurrent() abort
  let cur = goda_cmp#get_current()
  call writefile([json_encode({'current': {
        \ 'mark': get(get(cur.sourceOptions, 'words', {}), 'mark', ''),
        \ 'visible': goda_cmp#visible()}})], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-x>3 <Cmd>call DumpCurrent()<CR>
" words2 (an alias of the configuration, testdata/funcscfg): words with
" params of its own, its duplicates kept.
call goda_cmp#patch_global({
      \ 'sources': ['words', 'words2'],
      \ 'sourceParams': {'words2': {'minLength': 9}},
      \ 'sourceOptions': {'words2': {'dup': 'keep'}},
      \ 'contextFiletype': 'treesitter',
      \ })
call goda_cmp#patch_filetype('lua', {'sourceOptions': {'words': {'mark': 'LUA'}}})
