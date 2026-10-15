if exists('g:loaded_goda_cmp')
  finish
endif
let g:loaded_goda_cmp = 1

command! GodaCmpBuild call goda_cmp#build()
command! GodaCmpEnable call goda_cmp#enable()
command! GodaCmpDisable call goda_cmp#disable()
