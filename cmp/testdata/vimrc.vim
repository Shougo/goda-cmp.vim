" vimrc of the real-Vim end-to-end test (cmp/e2e_test.go).
set nocompatible noswapfile nobackup nowritebackup ttimeout ttimeoutlen=10
" $GODA_COMPLETEOPT lets a test run with other completeopt values.
let &completeopt = empty($GODA_COMPLETEOPT) ? 'menuone,noinsert,noselect' : $GODA_COMPLETEOPT
let &runtimepath = $GODA_RTP . ',' . $GODA_CMP_RTP . (empty($PUM_RTP) ? '' : ',' . $PUM_RTP) . ',' . &runtimepath
let g:goda_cmp#binary = $GODA_CMP_BIN
filetype plugin on

" Ctrl-] in insert mode records what the popup shows.
function! DumpPopup() abort
  let ui = empty($PUM_RTP) ? 'native' : 'pum'
  if ui ==# 'pum'
    let visible = pum#visible()
    let words = visible ? map(copy(pum#_get().items), 'v:val.word') : []
  else
    let visible = pumvisible()
    " What the user sees: complete_info().items comes back in reverse order
    " (checked in Vim 9.1), so read the popup rows from the screen instead.
    " pum_getpos() says where the popup is (it may open above the cursor)
    " and how many rows it shows (fewer than the items when they do not fit).
    let words = []
    if visible
      let pos = pum_getpos()
      for i in range(pos.height)
        let row = join(map(range(pos.col + 1, pos.col + pos.width),
              \ {_, c -> screenstring(pos.row + 1 + i, c)}), '')
        call add(words, split(trim(row))[0])
      endfor
    endif
  endif
  call writefile([json_encode({'visible': visible ? v:true : v:false, 'words': words, 'line': getline('.')})], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-]> <Cmd>call DumpPopup()<CR>

" The pum scenario switches the popup from Vim script.
if !empty($PUM_RTP)
  call goda_cmp#patch_global({'ui': 'pum'})
endif
autocmd VimEnter * call goda_cmp#enable()

" Run-time settings, as in a ddc.vim configuration that patches the buffer
" while the command line is used: Ctrl-G switches the sources off for this
" buffer, Ctrl-T puts the previous settings back, Ctrl-Y tries an invalid patch
" and records the error.
function! PatchOff() abort
  let g:prev_config = goda_cmp#get_buffer()
  call goda_cmp#patch_buffer({'sources': []})
  return ''
endfunction
function! PatchRestore() abort
  call goda_cmp#set_buffer(g:prev_config)
  return ''
endfunction
function! PatchBad() abort
  try
    call goda_cmp#patch_buffer({'sources': ['no_such_source']})
    let err = 'no error'
  catch
    let err = v:exception
  endtry
  call writefile([json_encode({'error': err, 'buffer': goda_cmp#get_buffer()})], $GODA_OUT, 'a')
  return ''
endfunction
" $GODA_NO_PATCH_MAPS leaves the keys alone (Ctrl-Y accepts a candidate).
if empty($GODA_NO_PATCH_MAPS)
  inoremap <C-g> <Cmd>call PatchOff()<CR>
  inoremap <C-t> <Cmd>call PatchRestore()<CR>
  inoremap <C-y> <Cmd>call PatchBad()<CR>
endif
