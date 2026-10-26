" For TestRealTerminal: completion in a terminal.
autocmd VimEnter * call goda_cmp#enable_terminal()
" (:terminal takes "|" as part of its command: a function.)
function! GodaTerm() abort
  if has('nvim')
    terminal sh
    startinsert
  else
    terminal ++curwin sh
  endif
endfunction
command! GodaTerm call GodaTerm()
