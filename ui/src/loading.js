// (A) SHOW & HIDE SPINNER
function show () {
    document.getElementById("spinner").classList.add("show");
  }
  function hide () {
    document.getElementById("spinner").classList.remove("show");
  }

  // (B) AJAX DEMO - USE HTTP:// NOT FILE://
function demoAJAX () {
    show(); // BLOCK PAGE WHILE LOADING
    var xhr = new XMLHttpRequest();
    xhr.open("GET", "loading.html");
    xhr.onload = function(){
      document.getElementById("container").innerHTML = this.response;
      hide(); // UNBLOCK PAGE
    };
    xhr.send();
  }
   
  // (C) AJAX DEMO - WITH FETCH
  function demoFETCH () {
    show(); // BLOCK PAGE WHILE LOADING
    fetch("loading.html")
    .then(res => res.text)
    .then((txt) => { document.getElementById("container").innerHTML = txt; })
    .catch((err) => { console.error(err); })
    .finally(() => { hide(); }); // UNBLOCK PAGE
  }