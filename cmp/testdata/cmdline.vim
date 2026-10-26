" For TestRealCmdline: ":" enables command-line completion for that command
" line, as in ddc.vim's documentation, and its end is recorded.
nnoremap : <Cmd>call goda_cmp#enable_cmdline()<CR>:
autocmd User GodaCmpCmdlineLeave
      \ call writefile([json_encode({'left': v:true})], $GODA_OUT, 'a')
