" The vimrc of the end-to-end tests (package e2e). A test's own settings come
" from $GODA_VIMRC, sourced before goda_cmp#enable() runs at VimEnter.
set nocompatible noswapfile nobackup nowritebackup ttimeout ttimeoutlen=10
" Neovim's defaults differ from Vim's: keep what is typed as it is typed.
set noautoindent nosmarttab
" $GODA_COMPLETEOPT lets a test run with other completeopt values.
let &completeopt = empty($GODA_COMPLETEOPT) ? 'menuone,noinsert,noselect' : $GODA_COMPLETEOPT
let &runtimepath = join(filter([$GODA_RTP, $GODA_CMP_RTP, $GODA_TESTUI_RTP]
      \ + split($GODA_EXTRA_RTP, ','), '!empty(v:val)'), ',') . ',' . &runtimepath
let g:goda_cmp#binary = $GODA_CMP_BIN
filetype plugin on
" Neovim turns indenting on by default (Vim does not).
filetype indent off

" Ctrl-] records the line, the command line and what the UI shows: the test
" UI's state (cmptest.NewUI("test")) when $GODA_DUMP_UI is "test", Vim's own
" popup otherwise, or what GodaDumpUI() returns when a test defines it.
function! DumpPopup() abort
  " In a terminal, the line of the terminal's cursor (Vim keeps it apart).
  let line = mode() ==# 't' && !has('nvim')
        \ ? term_getline('', term_getcursor(bufnr('%'))[0]) : getline('.')
  let d = {'line': line, 'cmdline': mode() ==# 'c' ? getcmdline() : ''}
  if exists('*GodaDumpUI')
    call extend(d, GodaDumpUI())
  elseif $GODA_DUMP_UI ==# 'test'
    let s = goda_cmp_testui#test#state()
    call extend(d, {'visible': s.visible ? v:true : v:false,
          \ 'words': map(copy(s.items), 'v:val.word')})
  else
    call extend(d, GodaDumpNative())
  endif
  call writefile([json_encode(d)], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-]> <Cmd>call DumpPopup()<CR>
cnoremap <C-]> <Cmd>call DumpPopup()<CR>
tnoremap <C-]> <Cmd>call DumpPopup()<CR>

" Vim's popup menu, as the user sees it: complete_info().items comes back in
" reverse order (checked in Vim 9.1), so the rows are read from the screen.
" pum_getpos() says where the popup is (it may open above the cursor) and how
" many rows it shows (fewer than the items when they do not fit).
function! GodaDumpNative() abort
  let visible = pumvisible()
  let words = []
  if visible
    " Neovim answers with Floats.
    let pos = map(pum_getpos(), {_, v -> type(v) == v:t_float ? float2nr(v) : v})
    for i in range(pos.height)
      let row = join(map(range(pos.col + 1, pos.col + pos.width),
            \ {_, c -> screenstring(pos.row + 1 + i, c)}), '')
      call add(words, split(trim(row))[0])
    endfor
  endif
  return {'visible': visible ? v:true : v:false, 'words': words}
endfunction

" $GODA_PATCH_MAPS: run-time settings, as in a ddc.vim configuration that
" patches the buffer while the command line is used: Ctrl-G switches the
" sources off for this buffer, Ctrl-T puts the previous settings back, Ctrl-Y
" tries an invalid patch and records the error.
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
if !empty($GODA_PATCH_MAPS)
  inoremap <C-g> <Cmd>call PatchOff()<CR>
  inoremap <C-t> <Cmd>call PatchRestore()<CR>
  inoremap <C-y> <Cmd>call PatchBad()<CR>
endif

if !empty($GODA_VIMRC)
  source $GODA_VIMRC
endif
autocmd VimEnter * call goda_cmp#enable()
