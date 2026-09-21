<template>
  <section class="not-found page-width">
    <span>404</span>
    <h1>这一页还没有被写进故事里</h1>
    <p>地址可能有误，或者内容已经离开书架。</p>
    <RouterLink :to="{ name: 'market' }"><el-button type="primary" size="large">返回书市</el-button></RouterLink>
  </section>
</template>

