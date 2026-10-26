" A stand-in for pum.vim that only records what it is asked to open. It checks
" that goda-cmp calls the pum.vim API with the arguments pum.vim documents:
" pum#open(startcol, items). The real pum.vim needs Vim 9.1.0448 or newer.
let s:state = {'visible': v:false, 'items': [], 'startcol': 0}

function! pum#open(startcol, items, ...) abort
  let s:state = {'visible': !empty(a:items), 'items': a:items, 'startcol': a:startcol}
endfunction

function! pum#close(...) abort
  let s:state.visible = v:false
  let s:state.items = []
endfunction

function! pum#visible() abort
  return s:state.visible
endfunction

function! pum#skip_complete() abort
  return v:false
endfunction

function! pum#_get() abort
  return s:state
endfunction
