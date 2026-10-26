" The Vim side of cmptest.NewUI("test"): it shows nothing, and keeps what it
" was asked to show for the tests (goda_cmp_testui#test#state()).
let s:state = {'visible': v:false, 'items': [], 'col': 0, 'mode': ''}

function! goda_cmp_testui#test#show(r) abort
  if !goda_cmp#_fresh(a:r)
    return 0
  endif
  let s:state = {'visible': !empty(a:r.items), 'items': a:r.items,
        \ 'col': a:r.col, 'mode': a:r.mode}
  return 1
endfunction

function! goda_cmp_testui#test#hide() abort
  let s:state = {'visible': v:false, 'items': [], 'col': 0, 'mode': ''}
endfunction

function! goda_cmp_testui#test#skip(event) abort
  return v:false
endfunction

function! goda_cmp_testui#test#visible() abort
  return s:state.visible
endfunction

function! goda_cmp_testui#test#update_info(r) abort
  return 0
endfunction

function! goda_cmp_testui#test#state() abort
  return s:state
endfunction
