<?php

// Friendo theme markup

?>

<!doctype html>

<html lang="en">

    <head>
        <meta charset="utf-8">
        <title>The HTML5 Herald</title>
        <meta name="description" content="The HTML5 Herald">
        <meta name="author" content="SitePoint">
        <?php wp_head(); ?>
    </head>

    <body <?php body_class(); ?>>
        <div id="app"></div>
        <?php wp_footer(); ?>
    </body>

</html>