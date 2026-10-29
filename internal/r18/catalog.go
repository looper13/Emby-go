package r18

// CatalogQuery 接收 Emby 偏移分页条件；不支持的上游筛选会明确拒绝。
type CatalogQuery struct {
	Search, Sort, NameStartsWith, NameStartsWithOrGreater, NameLessThan string
	Years                                                               []int
	Descending                                                          bool
	Start, Limit                                                        int
}

// CatalogPage 没有伪造全库总数，只提供下一页是否存在。
type CatalogPage struct {
	Items   []Video
	HasMore bool
}
