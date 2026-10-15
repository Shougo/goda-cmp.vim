" vimrc of the real-Vim end-to-end test (cmp/e2e_test.go).
set nocompatible noswapfile nobackup nowritebackup ttimeout ttimeoutlen=10
set completeopt=menuone,noinsert,noselect
let &runtimepath = $GODA_RTP . ',' . $GODA_CMP_RTP . (empty($PUM_RTP) ? '' : ',' . $PUM_RTP) . ',' . &runtimepath
let g:goda_cmp_binary = $GODA_CMP_BIN
filetype plugin on

" Ctrl-] in insert mode records what the popup shows.
function! DumpPopup() abort
  let l:ui = empty($PUM_RTP) ? 'native' : 'pum'
  if l:ui ==# 'pum'
    let l:visible = pum#visible()
    let l:words = l:visible ? map(copy(pum#_get().items), 'v:val.word') : []
  else
    let l:visible = pumvisible()
    " What the user sees: complete_info().items comes back in reverse order
    " (checked in Vim 9.1), so read the popup rows from the screen instead.
    let l:words = []
    if l:visible
      let l:n = len(complete_info(['items']).items)
      let l:before = strpart(getline('.'), 0, col('.') - 1)
      let l:off = len(l:before) - len(matchstr(l:before, '\k*$'))
      for l:i in range(1, l:n)
        let l:row = join(map(range(1, 30), {_, c -> screenstring(screenrow() + l:i, c)}), '')
        call add(l:words, split(trim(strcharpart(l:row, l:off)))[0])
      endfor
    endif
  endif
  call writefile([json_encode({'visible': l:visible ? v:true : v:false, 'words': l:words, 'line': getline('.')})], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-]> <Cmd>call DumpPopup()<CR>

autocmd VimEnter * GodaCmpEnable
