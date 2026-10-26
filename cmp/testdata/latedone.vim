" For TestRealLateDone: a UI that tells the accepted item late, after more
" was typed (as pum.vim does when the next key closes its popup). The delay
" leaves the request for what was typed pending when Ctrl-X D tells it.
call goda_cmp#patch_global({'autoCompleteDelay': 500})
inoremap <C-x>d <Cmd>call goda_cmp#_done({'word': 'foobar', 'user_data': '1:0'}, 'accept')<CR>
